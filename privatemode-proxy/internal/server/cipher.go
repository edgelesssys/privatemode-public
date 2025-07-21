// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/edgelesssys/continuum/internal/gpl/crypto"
	"github.com/edgelesssys/continuum/internal/gpl/secretmanager"
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
func NewRenewableRequestCipher(sm secretManager, r *http.Request) (*RenewableRequestCipher, error) {
	c := &RenewableRequestCipher{sm: sm, rc: nil}
	err := c.init(r)
	if err != nil {
		return nil, fmt.Errorf("initializing renewable request cipher: %w", err)
	}
	return c, nil
}

// ResetSecret clears the cached RequestCipher, forcing re-initialization on next use.
func (c *RenewableRequestCipher) ResetSecret(r *http.Request) error {
	c.rc = nil
	err := c.sm.ForceUpdate(r.Context())
	if err != nil {
		return fmt.Errorf("forcing secret update: %w", err)
	}

	return c.init(r)
}

func (c *RenewableRequestCipher) init(r *http.Request) error {
	secret, err := c.sm.LatestSecret(r.Context())
	if err != nil {
		return fmt.Errorf("get latest secret: %w", err)
	}

	rc, err := crypto.NewRequestCipher(secret.Data, secret.ID)
	if err != nil {
		return fmt.Errorf("creating request cipher: %w", err)
	}

	c.rc = rc
	return nil
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
