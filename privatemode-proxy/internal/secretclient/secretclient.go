// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package secretclient implements a client to interact with the secret API.
package secretclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/edgelesssys/continuum/internal/oss/hpke"
	"github.com/edgelesssys/continuum/internal/oss/httpapi"
	"github.com/edgelesssys/continuum/internal/oss/secretexchange"
)

// Client is used to interact with Privatemode's API.
type Client struct {
	httpClient *http.Client
	endpoint   string
}

// New sets up a new [Client].
func New(httpClient *http.Client, endpoint string) *Client {
	return &Client{
		httpClient: httpClient,
		endpoint:   endpoint,
	}
}

// ExchangeSecret performs a cryptographic key agreement with the Privatemode backend.
func (c *Client) ExchangeSecret(ctx context.Context, meshCA *x509.Certificate, apiKey string) (string, []byte, error) {
	priv, err := hpke.MLKEM768X25519().GenerateKey()
	if err != nil {
		return "", nil, fmt.Errorf("generating key: %w", err)
	}
	req := httpapi.SecretReq{PublicKey: priv.PublicKey().Bytes()}

	// Do the request
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return "", nil, fmt.Errorf("marshaling request: %w", err)
	}
	body, err := httpapi.Do(ctx, c.httpClient, http.MethodPost, "https://"+c.endpoint+"/privatemode/v1/secret", reqJSON, apiKey)
	if err != nil {
		return "", nil, fmt.Errorf("doing request: %w", err)
	}
	var resp httpapi.SecretResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	// Verify signature with secret-service public key
	ssPublicKey, err := getVerifiedPublicKey(resp.MeshCert, meshCA)
	if err != nil {
		return "", nil, fmt.Errorf("getting public key: %w", err)
	}
	if !ecdsa.VerifyASN1(ssPublicKey, secretexchange.Hash(req.PublicKey, resp.EncapsulatedKey), resp.Signature) {
		return "", nil, errors.New("invalid signature")
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
