// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package updater contains the functionality to update prompt secrets when the underlying Privatemode deployment is updated.
package updater

import (
	"context"
	"crypto/x509"
	"fmt"
	"log/slog"
	"time"

	"github.com/avast/retry-go/v5"
)

// Updater implements how to update prompt secrets when the underlying Privatemode deployment is updated. It is not thread-safe.
type Updater struct {
	ssClient  ssClient
	log       *slog.Logger
	retryOpts []retry.Option
	caGetter  CAGetter
	meshCA    *x509.Certificate
}

type ssClient interface {
	ExchangeSecret(ctx context.Context, meshCA *x509.Certificate, apiKey string) (string, []byte, error)
}

// CAGetter gets the mesh CA.
type CAGetter interface {
	GetMeshCA(ctx context.Context, apiKey string) (*x509.Certificate, error)
}

// New returns a new secret updater.
func New(ssClient ssClient, caGetter CAGetter, log *slog.Logger) *Updater {
	retryOpts := []retry.Option{retry.Delay(1 * time.Second), retry.Attempts(3)}
	return &Updater{
		ssClient:  ssClient,
		caGetter:  caGetter,
		log:       log,
		retryOpts: retryOpts,
	}
}

// UpdateSecret implements how to update prompt secrets when the underlying Privatemode deployment is updated.
func (s *Updater) UpdateSecret(ctx context.Context, apiKey string) (id string, data []byte, retErr error) {
	if err := retry.New(s.retryOpts...).Do(func() error {
		if ctx.Err() != nil {
			return retry.Unrecoverable(ctx.Err())
		}
		if s.meshCA == nil {
			if err := s.updateCA(ctx, apiKey); err != nil {
				return err
			}
		}
		var err error
		id, data, err = s.ssClient.ExchangeSecret(ctx, s.meshCA, apiKey)
		if err == nil {
			return nil
		}
		s.log.Error("Set secrets", "error", err)
		if err := s.updateCA(ctx, apiKey); err != nil {
			return err
		}
		id, data, err = s.ssClient.ExchangeSecret(ctx, s.meshCA, apiKey)
		return err
	}); err != nil {
		return "", nil, fmt.Errorf("setting secrets: %w", err)
	}
	return id, data, nil
}

func (s *Updater) updateCA(ctx context.Context, apiKey string) error {
	cert, err := s.caGetter.GetMeshCA(ctx, apiKey)
	if err != nil {
		return fmt.Errorf("updating mesh CA: %w", err)
	}
	s.meshCA = cert
	return nil
}

// StaticCAGetter gets the mesh CA, expecting a static manifest.
type StaticCAGetter struct {
	caUpdater       caUpdater
	expectedMfBytes []byte
}

// NewStaticCAGetter returns a new StaticCertGetter.
func NewStaticCAGetter(caUpdater caUpdater, expectedMfBytes []byte) StaticCAGetter {
	return StaticCAGetter{
		caUpdater:       caUpdater,
		expectedMfBytes: expectedMfBytes,
	}
}

// GetMeshCA gets the certificate.
func (s StaticCAGetter) GetMeshCA(ctx context.Context, apiKey string) (*x509.Certificate, error) {
	return s.caUpdater.GetAttestedMeshCA(ctx, s.expectedMfBytes, apiKey)
}

type caUpdater interface {
	GetAttestedMeshCA(ctx context.Context, expectedMfBytes []byte, apiKey string) (*x509.Certificate, error)
}
