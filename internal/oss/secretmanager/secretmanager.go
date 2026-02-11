// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package secretmanager manages the lifetime of a secret and always returns an up-to-date secret.
package secretmanager

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/httpapi"
	"k8s.io/utils/clock"
)

const (
	secretLifetime      = time.Hour
	secretRefreshBuffer = 5 * time.Minute
)

// SecretManager manages the lifetime of a secret and always returns an up-to-date secret.
type SecretManager struct {
	secret                   *Secret
	updateSecretFn           updateSecretFn
	mut                      sync.Mutex
	clock                    clock.Clock
	apiKey                   string
	apiKeyDropOnUnauthorized bool
	apiKeyChan               chan struct{} // signals the Loop that an API key has been set
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

type updateSecretFn func(ctx context.Context, apiKey string) (string, []byte, error)

// New creates a new SecretManager.
func New(updateSecretFn updateSecretFn, apiKeyDropOnUnauthorized bool) *SecretManager {
	return &SecretManager{
		updateSecretFn:           updateSecretFn,
		clock:                    clock.RealClock{},
		apiKeyDropOnUnauthorized: apiKeyDropOnUnauthorized,
		apiKeyChan:               make(chan struct{}),
	}
}

// LatestSecret returns the current secret. If the secret is older than the lifetime, a new secret is generated.
func (sm *SecretManager) LatestSecret(ctx context.Context) (Secret, error) {
	sm.mut.Lock()
	defer sm.mut.Unlock()

	if sm.apiKey == "" {
		return Secret{}, errors.New("don't have an API key yet")
	}

	now := sm.clock.Now()
	if sm.secret == nil || !now.Before(sm.secret.expirationDate) { // should trigger on expiration date and not after
		if err := sm.updateSecret(ctx, now, sm.apiKey); err != nil {
			return Secret{}, err
		}
	}
	return *sm.secret, nil
}

// ForceUpdate forces an immediate secret update, regardless of expiration status.
func (sm *SecretManager) ForceUpdate(ctx context.Context) error {
	sm.mut.Lock()
	defer sm.mut.Unlock()

	return sm.updateSecret(ctx, sm.clock.Now(), sm.apiKey)
}

// OfferAPIKey offers an API key to the SecretManager.
// If the SecretManager already has one, this is a no-op.
func (sm *SecretManager) OfferAPIKey(ctx context.Context, apiKey string) error {
	sm.mut.Lock()
	defer sm.mut.Unlock()
	if sm.apiKey != "" {
		return nil
	}
	if err := sm.updateSecret(ctx, sm.clock.Now(), apiKey); err != nil {
		return err
	}
	sm.apiKey = apiKey
	close(sm.apiKeyChan) // signal the Loop that an API key has been set
	return nil
}

// Loop keeps the secret up-to-date by periodically updating it.
func (sm *SecretManager) Loop(ctx context.Context, log *slog.Logger) error {
	// wait for API key
	select {
	case <-ctx.Done():
		log.Info("Context cancelled, stopping loop")
		return nil
	case <-sm.apiKeyChan:
	}

	for {
		secret, err := sm.LatestSecret(ctx)
		if err != nil {
			if ctx.Err() != nil {
				log.Info("Context cancelled, stopping loop")
				return nil //nolint:nilerr
			}
			log.Error("Failed to updated secret", "error", err)
			return err
		}
		log.Info("Secret updated successfully")

		select {
		case <-ctx.Done():
			log.Info("Context cancelled, stopping loop")
			return nil
		case <-sm.clock.After(secret.expirationDate.Sub(sm.clock.Now())):
		}
	}
}

func (sm *SecretManager) updateSecret(ctx context.Context, now time.Time, apiKey string) error {
	id, data, err := sm.updateSecretFn(ctx, apiKey)
	if sm.apiKeyDropOnUnauthorized && errors.Is(err, httpapi.ErrUnauthorized) {
		sm.apiKey = ""
	}
	if err != nil {
		return err
	}
	sm.secret = &Secret{
		ID:   id,
		Data: data,
		// Clock.Now() returns the current time with monotonic time. Some operations on monotonic
		// times do not work on MacOS as the OS stops the monotonic clock when the system goes
		// to sleep. This leads to expiration time comparison failure after sleep.
		// To prevent this, we must remove the monotonic part using Round(0).
		// Cf. https://pkg.go.dev/time#hdr-Monotonic_Clocks
		expirationDate: now.Round(0).Add(secretLifetime - secretRefreshBuffer),
	}
	return nil
}
