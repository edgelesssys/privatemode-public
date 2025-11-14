// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/edgelesssys/continuum/internal/oss/crypto"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
)

// RenewableRequestCipher wraps a RequestCipher and that can be renewed when needed.
type RenewableRequestCipher struct {
	sm secretManager
	rc *crypto.RequestCipher
}

type secretManager interface {
	LatestSecret(ctx context.Context) (secretmanager.Secret, error)
	ForceUpdate(ctx context.Context) error
}

// NewRenewableRequestCipher creates a new RenewableRequestCipher with the given secretManager.
func NewRenewableRequestCipher(sm secretManager, r *http.Request) (*RenewableRequestCipher, *secretmanager.Secret, error) {
	c := &RenewableRequestCipher{sm: sm, rc: nil}
	secret, err := c.init(r)
	if err != nil {
		return nil, nil, fmt.Errorf("initializing renewable request cipher: %w", err)
	}
	return c, secret, nil
}

// ResetSecret clears the cached RequestCipher, forcing re-initialization on next use.
func (c *RenewableRequestCipher) ResetSecret(r *http.Request) (*secretmanager.Secret, error) {
	c.rc = nil
	err := c.sm.ForceUpdate(r.Context())
	if err != nil {
		return nil, fmt.Errorf("forcing secret update: %w", err)
	}

	return c.init(r)
}

func (c *RenewableRequestCipher) init(r *http.Request) (*secretmanager.Secret, error) {
	secret, err := c.sm.LatestSecret(r.Context())
	if err != nil {
		return nil, fmt.Errorf("get latest secret: %w", err)
	}

	rc, err := crypto.NewRequestCipher(secret.Data, secret.ID)
	if err != nil {
		return nil, fmt.Errorf("creating request cipher: %w", err)
	}

	c.rc = rc
	return &secret, nil
}

// Encrypt encrypts the given plaintext using the wrapped RequestCipher.
func (c *RenewableRequestCipher) Encrypt(plaintext string) (string, error) {
	if c.rc == nil {
		return "", fmt.Errorf("request cipher is nil")
	}
	return c.rc.Encrypt(plaintext)
}

// DecryptResponse decrypts the given ciphertext using the wrapped RequestCipher.
func (c *RenewableRequestCipher) DecryptResponse(ciphertext string) (string, error) {
	if c.rc == nil {
		return "", fmt.Errorf("request cipher is nil")
	}
	return c.rc.DecryptResponse(ciphertext)
}
