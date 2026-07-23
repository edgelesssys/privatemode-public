// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package privatemode provides a client for interacting with a Privatemode deployment, such as secret exchange or encryption of inference requests.
package privatemode

import (
	"bytes"
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
	"github.com/edgelesssys/continuum/internal/oss/encbody"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/httputil"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/requestid"
	"github.com/edgelesssys/continuum/internal/oss/secretclient"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager/updater"
	contrastsdk "github.com/edgelesssys/contrast/sdk"
)

// ResponseError is returned when the API responds with a non-2xx
// status code. It preserves the response body and headers.
type ResponseError struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// RequestOptions configures an API request.
type RequestOptions struct {
	// Header contains additional request headers. Headers controlled by the
	// Privatemode client take precedence over values supplied here.
	Header http.Header
}

// Response contains a successful API response.
type Response struct {
	Header http.Header
	Body   []byte
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("unexpected status code %d: %s", e.StatusCode, e.Body)
}

// newResponseError reads the response body and wraps it as a [ResponseError].
func newResponseError(resp *http.Response) (*ResponseError, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading error response for status code %s: %w", resp.Status, err)
	}
	return &ResponseError{StatusCode: resp.StatusCode, Header: resp.Header, Body: body}, nil
}

// decryptResponseErrorBody attempts to decrypt the body of respErr via ce
// when the response declares an encrypted body. It returns a new ResponseError
// with the decrypted body on success, or respErr unchanged if not encrypted.
// On decryption or read failure, it returns a non-nil error.
func decryptResponseErrorBody(respErr *ResponseError, ce *encbody.ClientExchange) (*ResponseError, error) {
	if !encbody.IsEncrypted(respErr.Header) {
		return respErr, nil
	}
	encResp := &http.Response{
		StatusCode: respErr.StatusCode,
		Header:     respErr.Header,
		Body:       io.NopCloser(bytes.NewReader(respErr.Body)),
	}
	decResp, err := ce.DecryptResponse(encResp, constants.MaxEncryptedFrameSize)
	if err != nil {
		return nil, err
	}
	plainBody, err := io.ReadAll(decResp.Body)
	_ = decResp.Body.Close()
	if err != nil {
		return nil, err
	}
	return &ResponseError{
		StatusCode: respErr.StatusCode,
		Header:     decResp.Header,
		Body:       plainBody,
	}, nil
}

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

	// cred is the credential used for authentication with the
	// Privatemode deployment. It may be an API key, a Clerk JWT, or
	// an anonymous credential, and can be updated at runtime with
	// [Client.SetCredential].
	cred auth.Credential

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

// New creates a new Privatemode client authenticating with the given credential.
//
// The credential can be updated at runtime with [Client.SetCredential].
//
// Pass the zero [auth.Credential for a client that only performs
// unauthenticated operations such as fetching the manifest.
//
// Logging is disabled by default, but can be enabled by setting a
// logger with [Client.WithLogger].
func New(cred auth.Credential) *Client {
	c := &Client{
		cdnBaseURL:      "https://cdn.confidential.cloud/privatemode/v2",
		apiBaseURL:      "https://api.privatemode.ai",
		cred:            cred,
		log:             slog.New(slog.DiscardHandler),
		httpClient:      httputil.NewClient(),
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

// SetCredential updates the authentication credential used for subsequent
// requests. This can be called at any time, including after [Client.Initialize],
// to rotate short-lived credentials such as JWTs.
func (c *Client) SetCredential(cred auth.Credential) {
	c.cred = cred
	if c.secretManager != nil {
		c.secretManager.SetCredential(cred)
	}
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
	c.log.DebugContext(ctx, "Fetching manifest from CDN", "url", manifestURL)
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
// as inference requests, and may be called again to re-initialize the client with a new manifest.
func (c *Client) Initialize(ctx context.Context, expectedManifest []byte) error {
	c.log.DebugContext(ctx, "Initializing Privatemode client")

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

	c.log.DebugContext(ctx, "Offering credential")
	if err := c.secretManager.OfferCredential(ctx, c.cred); err != nil {
		return fmt.Errorf("offering credential to secret manager: %w", err)
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
	resp, err := c.ListModelsWithOptions(ctx, RequestOptions{})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// ListModelsWithOptions fetches the list of available models from the API and
// preserves the response headers.
func (c *Client) ListModelsWithOptions(ctx context.Context, opts RequestOptions) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBaseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header = cloneRequestHeader(opts.Header)
	resp, err := c.doAPIRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	return &Response{Header: resp.Header.Clone(), Body: respBody}, nil
}

func cloneRequestHeader(header http.Header) http.Header {
	if header == nil {
		return make(http.Header)
	}
	return header.Clone()
}

// doAPIRequest sets common Privatemode headers on req, sends it, and
// returns the response. The caller is responsible for closing the
// response body. An [ResponseError] is returned for non-2xx status codes.
func (c *Client) doAPIRequest(req *http.Request) (*http.Response, error) {
	c.cred.Apply(req.Header)
	req.Header.Set(constants.PrivatemodeVersionHeader, constants.Version())
	req.Header.Set(constants.PrivatemodeClientHeader, constants.PrivatemodeClientSDK)
	req.Header.Set(constants.PrivatemodeSecretIDHeader, c.currentSecret.ID)
	req.Header.Set(requestid.UserHeader, "sdk_"+requestid.New())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("doing request: %w", err)
	}

	if !(200 <= resp.StatusCode && resp.StatusCode < 300) {
		defer resp.Body.Close()
		respErr, err := newResponseError(resp)
		if err != nil {
			return nil, err
		}
		return nil, respErr
	}

	return resp, nil
}

// doEncryptedAPIRequest sends a request while applying the full encrypted-body
// request/response pipeline. Sending is performed by [Client.doAPIRequest].
// The caller is responsible for closing the response body.
// A [ResponseError] is returned for non-2xx status codes, and the error
// response is decrypted.
func (c *Client) doEncryptedAPIRequest(req *http.Request) (*forwarder.StreamingResponse, error) {
	if len(c.currentSecret.Data) == 0 {
		return nil, fmt.Errorf("no secret available: call Initialize() and UpdateSecret() first")
	}

	ce, err := encbody.InitializeClientExchange(c.currentSecret.Data)
	if err != nil {
		return nil, fmt.Errorf("initializing encrypted exchange: %w", err)
	}
	if err := ce.EncryptRequest(req); err != nil {
		return nil, fmt.Errorf("encrypting request: %w", err)
	}

	resp, err := c.doAPIRequest(req)
	if err != nil {
		if respErr, ok := errors.AsType[*ResponseError](err); ok {
			decryptedErr, decryptErr := decryptResponseErrorBody(respErr, ce)
			if decryptErr != nil {
				return nil, fmt.Errorf(
					"decrypting error response for status code %d: %w",
					respErr.StatusCode, decryptErr,
				)
			}
			return nil, decryptedErr
		}
		return nil, err
	}

	plainResp, err := ce.DecryptResponse(resp, constants.MaxEncryptedFrameSize)
	if err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("decrypting response: %w", err)
	}
	return plainResp, nil
}

type attestedMeshCAAdapter struct {
	attestGetter attest.Getter
	expectedMf   []byte
}

func (a attestedMeshCAAdapter) GetMeshCA(ctx context.Context, cred auth.Credential) (*x509.Certificate, error) {
	return a.attestGetter.GetAttestedMeshCA(ctx, a.expectedMf, cred)
}
