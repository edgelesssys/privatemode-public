// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package secretclient implements a client to interact with the secret API.
package secretclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/hpke"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/edgelesssys/continuum/internal/oss/httpapi"
	"github.com/edgelesssys/continuum/internal/oss/secretexchange"
)

const (
	// SecretPath is the API path for exchanging an inference secret.
	SecretPath = "/privatemode/v1/secret"
	// MaxRequestBodyBytes is the maximum accepted size of a secret exchange request body.
	MaxRequestBodyBytes = 16 * 1024 // 16 KiB
	// MaxBodySizeExceededMsg is the error message returned when the request body exceeds the maximum size.
	MaxBodySizeExceededMsg = "maximum request size exceeded"
)

// Client is used to interact with Privatemode's API.
type Client struct {
	httpClient       *http.Client
	endpoint         string
	extraHTTPHeaders http.Header
}

// New sets up a new [Client].
func New(httpClient *http.Client, endpoint string, extraHTTPHeaders http.Header) *Client {
	return &Client{
		httpClient:       httpClient,
		endpoint:         endpoint,
		extraHTTPHeaders: extraHTTPHeaders,
	}
}

// ExchangeSecret performs a cryptographic key agreement with the Privatemode backend.
func (c *Client) ExchangeSecret(ctx context.Context, meshCA *x509.Certificate, cred auth.Credential) (string, []byte, error) {
	return ExchangeSecret(ctx, c.httpClient, c.endpoint, cred, c.extraHTTPHeaders, func(req *httpapi.SecretReq, resp *httpapi.SecretResp) error {
		ssPublicKey, err := getVerifiedPublicKey(resp.MeshCert, meshCA)
		if err != nil {
			return fmt.Errorf("getting public key: %w", err)
		}
		if !ecdsa.VerifyASN1(ssPublicKey, secretexchange.Hash(req.PublicKey, resp.EncapsulatedKey), resp.Signature) {
			return errors.New("invalid signature")
		}
		return nil
	})
}

type responseVerifier func(*httpapi.SecretReq, *httpapi.SecretResp) error

// ExchangeSecret performs a cryptographic key agreement with the Privatemode backend.
func ExchangeSecret(
	ctx context.Context, httpClient *http.Client, endpoint string, cred auth.Credential, extraHTTPHeaders http.Header, verify responseVerifier,
) (string, []byte, error) {
	priv, err := hpke.MLKEM768X25519().GenerateKey()
	if err != nil {
		return "", nil, fmt.Errorf("generating key: %w", err)
	}
	req := &httpapi.SecretReq{PublicKey: priv.PublicKey().Bytes()}

	// Do the request
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return "", nil, fmt.Errorf("marshaling request: %w", err)
	}
	body, err := httpapi.Do(ctx, httpClient, http.MethodPost, "https://"+endpoint+SecretPath, reqJSON, cred, extraHTTPHeaders)
	if err != nil {
		return "", nil, fmt.Errorf("doing request: %w", err)
	}
	resp := &httpapi.SecretResp{}
	if err := json.Unmarshal(body, resp); err != nil {
		return "", nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	if err := verify(req, resp); err != nil {
		return "", nil, err
	}

	// Export the shared secret
	recipient, err := hpke.NewRecipient(resp.EncapsulatedKey, priv, hpke.HKDFSHA256(), hpke.ExportOnly(), nil)
	if err != nil {
		return "", nil, fmt.Errorf("creating recipient: %w", err)
	}
	secret, err := recipient.Export("", 32)
	if err != nil {
		return "", nil, fmt.Errorf("exporting secret: %w", err)
	}

	return secretexchange.ID(req.PublicKey), secret, nil
}

func getVerifiedPublicKey(certRaw []byte, parentCert *x509.Certificate) (*ecdsa.PublicKey, error) {
	cert, err := x509.ParseCertificate(certRaw)
	if err != nil {
		return nil, fmt.Errorf("parsing certificate: %w", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parentCert)
	if _, err := cert.Verify(x509.VerifyOptions{Roots: roots}); err != nil {
		return nil, fmt.Errorf("verifying certificate: %w", err)
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("public key is not ECDSA")
	}
	return pub, nil
}
