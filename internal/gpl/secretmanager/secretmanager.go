// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// package secretmanager manages the lifetime of a secret and always returns an up-to-date secret.
package secretmanager

import (
	"context"
	"crypto/rand"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SecretManager manages the lifetime of a secret and always returns an up-to-date secret.
type SecretManager struct {
	secret              *Secret
	secretLifetime      time.Duration
	secretRefreshBuffer time.Duration
	updateSecretFn      updateSecretFn
	mut                 sync.Mutex
}

// Secret includes all the information needed to identify and use a secret.
type Secret struct {
	ID             string
	Data           []byte
	expirationDate time.Time
}

// Map returns the secret as a map.
func (s Secret) Map() map[string][]byte {
	return map[string][]byte{
		s.ID: s.Data,
	}
}

type updateSecretFn func(ctx context.Context, secrets map[string][]byte, ttl time.Duration) error

// New creates a new SecretManager.
func New(
	updateSecretFn updateSecretFn,
	secretLifetime time.Duration, secretRefreshBuffer time.Duration,
) *SecretManager {
	return &SecretManager{
		secret:              nil,
		secretLifetime:      secretLifetime,
		secretRefreshBuffer: secretRefreshBuffer,
		updateSecretFn:      updateSecretFn,
		mut:                 sync.Mutex{},
	}
}

// LatestSecret returns the current secret. If the secret is older than the lifetime, a new secret is generated.
func (sm *SecretManager) LatestSecret(ctx context.Context, now time.Time) (Secret, error) {
	sm.mut.Lock()
	defer sm.mut.Unlock()
	if sm.secret == nil || !now.Before(sm.secret.expirationDate) { // should trigger on expiration date and not after
		if err := sm.updateSecret(ctx, now); err != nil {
			return Secret{}, err
		}
	}
	return *sm.secret, nil
}

// Loop keeps the secret up-to-date by periodically updating it.
func (sm *SecretManager) Loop(ctx context.Context, log *slog.Logger) error {
	log.Info("Fetching initial secret")
	now := time.Now()
	secret, err := sm.LatestSecret(ctx, now)
	if err != nil {
		log.Error("Initial fetch of secret", "error", err)
		return err
	}
	log.Info("Secret updated successfully")

	for {
		select {
		case <-ctx.Done():
			log.Info("Context cancelled, stopping loop")
			return nil
		case <-time.After(secret.expirationDate.Sub(now)):
			log.Info("Updating secret")
			now = time.Now()
			secret, err = sm.LatestSecret(ctx, now)
			if err != nil {
				if ctx.Err() != nil {
					log.Info("Context cancelled, stopping loop")
					return nil //nolint:nilerr
				}
				log.Error("Failed to update secret", "error", err)
				return err
			}
		}
	}
}

func (sm *SecretManager) updateSecret(ctx context.Context, now time.Time) error {
	// create secrets with a buffer to refresh the secrets before they expire in the AS
	secret, err := createRandom32ByteSecret(now.Add(sm.secretLifetime - sm.secretRefreshBuffer))
	if err != nil {
		return err
	}

	if err := sm.updateSecretFn(ctx, secret.Map(), sm.secretLifetime); err != nil {
		return err
	}
	sm.secret = secret
	return nil
}

// createRandom32ByteSecret creates a random 32 byte secret.
func createRandom32ByteSecret(expirationDate time.Time) (*Secret, error) {
	data := make([]byte, 32) // AES-256
	_, err := rand.Read(data)
	if err != nil {
		return nil, err
	}

	return &Secret{
		ID:             uuid.New().String(),
		Data:           data,
		expirationDate: expirationDate,
	}, nil
}
