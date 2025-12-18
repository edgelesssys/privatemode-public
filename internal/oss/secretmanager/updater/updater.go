// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package updater contains the functionality to update prompt secrets when the underlying Continuum deployment is updated.
package updater

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	"github.com/avast/retry-go/v5"
)

// Updater implements how to update prompt secrets when the underlying Continuum deployment is updated. It is not thread-safe.
type Updater struct {
	ssClient        ssClient
	log             *slog.Logger
	retryOpts       []retry.Option
	tlsConfigGetter tlsConfigGetter
}

type ssClient interface {
	SetSecrets(ctx context.Context, secrets map[string][]byte, ttl time.Duration) error
	SetTLSConfig(tlsConfig *tls.Config)
}

type tlsConfigGetter interface {
	GetTLSConfig(ctx context.Context) (*tls.Config, error)
}

// New returns a new secret updater.
func New(ssClient ssClient, tlsConfigGetter tlsConfigGetter, log *slog.Logger) *Updater {
	retryOpts := []retry.Option{retry.Delay(1 * time.Second), retry.Attempts(3)}
	return &Updater{
		ssClient:        ssClient,
		tlsConfigGetter: tlsConfigGetter,
		log:             log,
		retryOpts:       retryOpts,
	}
}

// UpdateSecrets implements how to update prompt secrets when the underlying Continuum deployment is updated.
func (s *Updater) UpdateSecrets(ctx context.Context, secrets map[string][]byte, ttl time.Duration) error {
	if err := retry.New(s.retryOpts...).Do(func() error {
		if ctx.Err() != nil {
			return retry.Unrecoverable(ctx.Err())
		}
		if err := s.ssClient.SetSecrets(ctx, secrets, ttl); err == nil {
			return nil
		} else {
			s.log.Error("Set secrets", "error", err)
		}
		if err := s.updateTLSConfig(ctx); err != nil {
			return fmt.Errorf("refreshing deployment: %w", err)
		}
		return s.ssClient.SetSecrets(ctx, secrets, ttl)
	}); err != nil {
		return fmt.Errorf("setting secrets: %w", err)
	}
	return nil
}

func (s *Updater) updateTLSConfig(ctx context.Context) error {
	tlsConfig, err := s.tlsConfigGetter.GetTLSConfig(ctx)
	if err != nil {
		return fmt.Errorf("updating TLS config: %w", err)
	}
	s.ssClient.SetTLSConfig(tlsConfig)
	return nil
}

// StaticTLSConfigGetter returns a static TLS config.
type StaticTLSConfigGetter struct {
	config *tls.Config
}

// NewStaticTLSConfigGetter returns a new StaticTLSConfigGetter.
func NewStaticTLSConfigGetter(tlsConfig *tls.Config) StaticTLSConfigGetter {
	return StaticTLSConfigGetter{config: tlsConfig}
}

// GetTLSConfig returns the static TLS config.
func (s StaticTLSConfigGetter) GetTLSConfig(_ context.Context) (*tls.Config, error) {
	return s.config, nil
}
