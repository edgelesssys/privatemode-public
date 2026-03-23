// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package privatemode provides a client for interacting with a Privatemode deployment, such as secret exchange or encryption of inference requests.
package privatemode

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/attest"
	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/secretclient"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager/updater"
	contrastsdk "github.com/edgelesssys/contrast/sdk"
	"github.com/google/uuid"
)

// Client is a client for interacting with a Privatemode deployment.
//
// It is not thread-safe.
type Client struct {
	// cdnBaseURL is the base URL of the Privatemode CDN.
	// Defaults to `https://cdn.confidential.cloud/privatemode/v2`,
	// but can be overridden with [Client.WithCDNBaseURL].
	cdnBaseURL string

	// apiBaseURL is the base URL of the Privatemode API
	// Gateway.
	// Defaults to `https://api.privatemode.ai`,
	// but can be overridden with [Client.WithAPIBaseURL].
	apiBaseURL string

	// apiKey is the API key used for authentication with the
	// Privatemode deployment.
	apiKey string

	// log is the main logger used by the client. It is also
	// passed to sub-clients.
	log *slog.Logger

	// httpClient is the HTTP client used for network requests.
	// It defaults to http.DefaultClient, but can be overridden for
	// testing with [Client.WithHTTPClient].
	httpClient *http.Client

	// currentSecret is the secret currently in use by the client.
	// It may not be up-to-date, in which case [Client.UpdateSecret]
	// should be called to update it.
	currentSecret secretmanager.Secret

	// TODO(msanft): At some point, the following clients should only
	// be consumed via the [Client]. Move these to this packages'
	// internal/ directory or remove them entirely once the [Client]
	// can be used in all places where their functionality is needed.

	// promptCacheSalt is a random salt generated at instantiation for
	// prompt caching. It lives as long as the client instance.
	promptCacheSalt string

	// secretManager is used for secret exchange with the Privatemode
	// deployment.
	secretManager *secretmanager.SecretManager
}

// New creates a new Privatemode client.
//
// Logging is disabled by default, but can be enabled by setting a
// logger with [Client.WithLogger].
func New(apiKey string) *Client {
	c := &Client{
		cdnBaseURL:      "https://cdn.confidential.cloud/privatemode/v2",
		apiBaseURL:      "https://api.privatemode.ai",
		apiKey:          apiKey,
		log:             slog.New(slog.DiscardHandler),
		httpClient:      http.DefaultClient,
		promptCacheSalt: openai.RandomPromptCacheSalt(),
	}

	return c
}

// WithCDNBaseURL sets the base URL for the CDN from which to fetch
// the manifest.
func (c *Client) WithCDNBaseURL(url string) *Client {
	c.cdnBaseURL = url
	return c
}

// WithAPIBaseURL sets the base URL for the API Gateway used by
// the client.
func (c *Client) WithAPIBaseURL(url string) *Client {
	c.apiBaseURL = url
	return c
}

// WithLogger sets the logger for the client.
func (c *Client) WithLogger(log *slog.Logger) *Client {
	c.log = log
	return c
}

// WithHTTPClient sets the HTTP client for the client.
func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	c.httpClient = httpClient
	return c
}

// FetchManifest fetches the manifest from the CDN.
func (c *Client) FetchManifest(ctx context.Context) ([]byte, error) {
	// Random query parameter is required to circumvent browser caching when called from the web app.
	// TODO(msanft): Consider disabling browser caching via response headers in S3 instead.
	manifestURL := c.cdnBaseURL + "/manifest.json?t=" + fmt.Sprint(time.Now().UnixMilli())
	c.log.Debug("Fetching manifest from CDN", "url", manifestURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("doing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %v: %s", resp.Status, body)
	}

	return body, nil
}

// Initialize the connection to the Privatemode deployment by setting
// up a secret for the client.
//
// This needs to be called before any crypto-requiring operations, such
// as inference requests.
func (c *Client) Initialize(ctx context.Context, expectedManifest []byte) error {
	c.log.Debug("Initializing Privatemode client")
	if c.secretManager == nil {
		c.log.Debug("No secret manager configured, initializing for the first time")

		apiURL, err := url.Parse(c.apiBaseURL)
		if err != nil {
			return fmt.Errorf("parsing API base URL: %w", err)
		}
		apiHost := apiURL.Host

		contrastClient := contrastsdk.New().WithSlog(c.log.WithGroup("contrast-sdk"))
		meshCAGetter := attest.NewGetter(c.httpClient, apiHost, contrastClient)
		meshCAAdapter := attestedMeshCAAdapter{meshCAGetter, expectedManifest}
		secretClient := secretclient.New(c.httpClient, apiHost)
		secretUpdater := updater.New(secretClient, meshCAAdapter, c.log.WithGroup("secret-updater"))

		// Creating a [Client] already requires an API key, so there's
		// no need to drop it on unauthorized errors.
		apiKeyDropOnUnauthorized := false

		c.secretManager = secretmanager.New(secretUpdater.UpdateSecret, apiKeyDropOnUnauthorized)
	}
	c.log.Debug("Offering API key")
	if err := c.secretManager.OfferAPIKey(ctx, c.apiKey); err != nil {
		return fmt.Errorf("offering API key to secret manager: %w", err)
	}
	return nil
}

// UpdateSecret updates the secret with the secret manager.
//
// Usually, users of the client will want to call this in some sort of
// loop to keep the secret up-to-date.
//
// [Client.Initialize] needs to have been called before calling this.
func (c *Client) UpdateSecret(ctx context.Context) error {
	if c.secretManager == nil {
		return fmt.Errorf("secret manager not initialized. Make sure to call Initialize() first")
	}
	secret, err := c.secretManager.LatestSecret(ctx)
	if err != nil {
		return fmt.Errorf("getting latest secret: %w", err)
	}
	c.currentSecret = secret
	return nil
}

// ExportSecret returns the current secret. It returns an error if no
// secret is set.
func (c *Client) ExportSecret() (secretmanager.Secret, error) {
	s := c.currentSecret
	if s.ID == "" {
		return secretmanager.Secret{}, fmt.Errorf("no secret set")
	}
	return s, nil
}

// ImportSecret imports a secret into the [Client].
// It returns an error if the ID or data is empty, or if the secret
// has already expired.
func (c *Client) ImportSecret(s secretmanager.Secret) error {
	if s.ID == "" {
		return fmt.Errorf("secret ID must not be empty")
	}
	if len(s.Data) == 0 {
		return fmt.Errorf("secret data must not be empty")
	}
	if !time.Now().Before(s.ExpirationDate) {
		return fmt.Errorf("secret has already expired (expiration: %v)", s.ExpirationDate)
	}
	c.currentSecret = s
	return nil
}

// ListModels fetches the list of available models from the API.
func (c *Client) ListModels(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBaseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	return c.doAPIRequestAndReadBody(req)
}

// doAPIRequestAndReadBody sets common Privatemode headers on req,
// sends it, and returns the full response body. If the body should
// not be fully read into memory, use [Client.doAPIRequest] instead.
func (c *Client) doAPIRequestAndReadBody(req *http.Request) ([]byte, error) {
	resp, err := c.doAPIRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	return respBody, nil
}

// doAPIRequest sets common Privatemode headers on req, sends it, and
// returns the response. The caller is responsible for closing the
// response body. An error is returned for non-2xx status codes,
// in which case the response body is closed by this function.
func (c *Client) doAPIRequest(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", auth.Bearer, c.apiKey))
	req.Header.Set(constants.PrivatemodeVersionHeader, constants.Version())
	req.Header.Set(constants.PrivatemodeClientHeader, constants.PrivatemodeClientSDK)
	req.Header.Set(constants.PrivatemodeSecretIDHeader, c.currentSecret.ID)
	req.Header.Set(constants.RequestIDHeader, "sdk_"+uuid.New().String())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("doing request: %w", err)
	}

	if !(200 <= resp.StatusCode && resp.StatusCode < 300) {
		defer resp.Body.Close()
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, errors.Join(
				fmt.Errorf("unexpected status code %d", resp.StatusCode),
				fmt.Errorf("reading response body: %w", readErr),
			)
		}
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, respBody)
	}

	return resp, nil
}

type attestedMeshCAAdapter struct {
	attestGetter attest.Getter
	expectedMf   []byte
}

func (a attestedMeshCAAdapter) GetMeshCA(ctx context.Context, apiKey string) (*x509.Certificate, error) {
	return a.attestGetter.GetAttestedMeshCA(ctx, a.expectedMf, apiKey)
}
