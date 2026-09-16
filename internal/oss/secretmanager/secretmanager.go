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

	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/edgelesssys/continuum/internal/oss/httpapi"
	"k8s.io/utils/clock"
)

const (
	secretLifetime      = time.Hour
	secretRefreshBuffer = 15 * time.Minute
)

// SecretManager manages the lifetime of a secret and always returns an up-to-date secret.
type SecretManager struct {
	secret                 *Secret
	updateSecretFn         updateSecretFn
	mut                    sync.Mutex
	clock                  clock.Clock
	credential             auth.Credential
	dropCredOnUnauthorized bool
	credChan               chan struct{} // signals the Loop that a credential has been set
	credChanClosed         bool
}

// Secret includes all the information needed to identify and use a secret.
type Secret struct {
	ID             string
	Data           []byte
	ExpirationDate time.Time
}

// Map returns the secret as a map.
func (s Secret) Map() map[string][]byte {
	return map[string][]byte{
		s.ID: s.Data,
	}
}

type updateSecretFn func(ctx context.Context, cred auth.Credential) (string, []byte, error)

// New creates a new SecretManager.
func New(updateSecretFn updateSecretFn, dropCredOnUnauthorized bool) *SecretManager {
	return &SecretManager{
		updateSecretFn:         updateSecretFn,
		clock:                  clock.RealClock{},
		dropCredOnUnauthorized: dropCredOnUnauthorized,
		credChan:               make(chan struct{}),
	}
}

// LatestSecret returns the current secret. If the secret is older than the lifetime, a new secret is generated.
func (sm *SecretManager) LatestSecret(ctx context.Context) (Secret, error) {
	sm.mut.Lock()
	defer sm.mut.Unlock()

	if sm.credential.IsZero() {
		return Secret{}, errors.New("don't have a credential yet")
	}

	now := sm.clock.Now()
	if sm.secret == nil || !now.Before(sm.secret.ExpirationDate) { // should trigger on expiration date and not after
		if err := sm.updateSecret(ctx, now, sm.credential); err != nil {
			return Secret{}, err
		}
	}
	return *sm.secret, nil
}

// ForceUpdate forces an immediate secret update, regardless of expiration status.
func (sm *SecretManager) ForceUpdate(ctx context.Context) error {
	sm.mut.Lock()
	defer sm.mut.Unlock()

	return sm.updateSecret(ctx, sm.clock.Now(), sm.credential)
}

// OfferCredential offers an authentication credential to the SecretManager.
// If the SecretManager already has one, this is a no-op.
func (sm *SecretManager) OfferCredential(ctx context.Context, cred auth.Credential) error {
	sm.mut.Lock()
	defer sm.mut.Unlock()
	if !sm.credential.IsZero() {
		return nil
	}
	if err := sm.updateSecret(ctx, sm.clock.Now(), cred); err != nil {
		return err
	}
	sm.setCredentialLocked(cred)
	return nil
}

// SetCredential replaces the credential used for subsequent secret updates. Unlike
// [SecretManager.OfferCredential], it overwrites an existing credential, which is needed
// for short-lived credentials (e.g. JWTs) that must be refreshed over time.
func (sm *SecretManager) SetCredential(cred auth.Credential) {
	sm.mut.Lock()
	defer sm.mut.Unlock()
	sm.setCredentialLocked(cred)
}

// setCredentialLocked stores cred and signals the Loop that a credential is available.
// The caller must hold sm.mut.
func (sm *SecretManager) setCredentialLocked(cred auth.Credential) {
	sm.credential = cred
	if !sm.credChanClosed && !cred.IsZero() {
		sm.credChanClosed = true
		close(sm.credChan) // signal the Loop that a credential has been set
	}
}

// Loop keeps the secret up-to-date by periodically updating it.
func (sm *SecretManager) Loop(ctx context.Context, log *slog.Logger) error {
	// wait for a credential
	select {
	case <-ctx.Done():
		log.InfoContext(ctx, "Context cancelled, stopping loop")
		return nil
	case <-sm.credChan:
	}

	const initialBackoff = 5 * time.Second
	const maxBackoff = 60 * time.Second
	backoff := initialBackoff
	for {
		secret, err := sm.LatestSecret(ctx)
		var wait time.Duration
		if err != nil {
			if ctx.Err() != nil {
				log.InfoContext(ctx, "Context cancelled, stopping loop")
				return nil //nolint:nilerr
			}
			log.ErrorContext(ctx, "Failed to update secret", "error", err, "backoff", backoff)
			wait = backoff
			backoff = min(backoff*2, maxBackoff)
		} else {
			log.InfoContext(ctx, "Secret updated successfully")
			backoff = initialBackoff
			wait = secret.ExpirationDate.Sub(sm.clock.Now())
		}

		select {
		case <-ctx.Done():
			log.InfoContext(ctx, "Context cancelled, stopping loop")
			return nil
		case <-sm.clock.After(wait):
		}
	}
}

func (sm *SecretManager) updateSecret(ctx context.Context, now time.Time, cred auth.Credential) error {
	id, data, err := sm.updateSecretFn(ctx, cred)
	if sm.dropCredOnUnauthorized && errors.Is(err, httpapi.ErrUnauthorized) {
		sm.credential = auth.Credential{}
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
		ExpirationDate: now.Round(0).Add(secretLifetime - secretRefreshBuffer),
	}
	return nil
}
