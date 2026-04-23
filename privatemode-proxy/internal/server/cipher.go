// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"fmt"

	"github.com/edgelesssys/continuum/internal/oss/crypto"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
)

// RenewableRequestCipher wraps a RequestCipher and that can be renewed when needed.
type RenewableRequestCipher struct {
	sm     secretManager
	rc     *crypto.RequestCipher
	secret *secretmanager.Secret
}

type secretManager interface {
	LatestSecret(ctx context.Context) (secretmanager.Secret, error)
	ForceUpdate(ctx context.Context) error
	OfferAPIKey(context.Context, string) error
}

// NewRenewableRequestCipher creates a new RenewableRequestCipher with the given secretManager.
func NewRenewableRequestCipher(ctx context.Context, sm secretManager) (*RenewableRequestCipher, error) {
	c := &RenewableRequestCipher{sm: sm, rc: nil}
	err := c.init(ctx)
	if err != nil {
		return nil, fmt.Errorf("initializing renewable request cipher: %w", err)
	}
	return c, nil
}

// ResetSecret clears the cached RequestCipher, forcing re-initialization on next use.
func (c *RenewableRequestCipher) ResetSecret(ctx context.Context) error {
	c.rc = nil
	c.secret = nil
	err := c.sm.ForceUpdate(ctx)
	if err != nil {
		return fmt.Errorf("forcing secret update: %w", err)
	}

	return c.init(ctx)
}

// Reinitialize re-initializes the cached RequestCipher, only updating the secret if it is no
// longer valid.
func (c *RenewableRequestCipher) Reinitialize(ctx context.Context) error {
	return c.init(ctx)
}

// GetSecret returns the secret in use by the cached RequestCipher. An error is returned if none
// has been initialized yet.
func (c *RenewableRequestCipher) GetSecret() (secretmanager.Secret, error) {
	if c.secret == nil {
		return secretmanager.Secret{}, fmt.Errorf("RenewableRequestCipher not initialized")
	}
	return *c.secret, nil
}

func (c *RenewableRequestCipher) init(ctx context.Context) error {
	secret, err := c.sm.LatestSecret(ctx)
	if err != nil {
		return fmt.Errorf("get latest secret: %w", err)
	}

	rc, err := crypto.NewRequestCipher(secret.Data, secret.ID)
	if err != nil {
		return fmt.Errorf("creating request cipher: %w", err)
	}

	c.rc = rc
	c.secret = &secret
	return nil
}

// Encrypt encrypts the given plaintext using the wrapped RequestCipher.
func (c *RenewableRequestCipher) Encrypt(plaintext string) (string, error) {
	if c.rc == nil {
		return "", fmt.Errorf("RenewableRequestCipher not initialized")
	}
	return c.rc.Encrypt(plaintext)
}

// DecryptResponse decrypts the given ciphertext using the wrapped RequestCipher.
func (c *RenewableRequestCipher) DecryptResponse(ciphertext string) (string, error) {
	if c.rc == nil {
		return "", fmt.Errorf("RenewableRequestCipher not initialized")
	}
	return c.rc.DecryptResponse(ciphertext)
}
