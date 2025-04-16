// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package setup

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"

	"github.com/edgelesssys/continuum/privatemode-proxy/internal/manifestlog"
	"github.com/spf13/afero"
)

const manifest = "manifest.json"

// tlsConfigAdapter updates the TLS config with the interface for the secretupdater.
type tlsConfigAdapter struct {
	fetcher          contrastDeploymentFetcher
	mfLogger         mfLogger
	tlsConfigUpdater tlsConfigUpdater
	log              *slog.Logger
}

type tlsConfigUpdater interface {
	GetTLSConfig(ctx context.Context, expectedMfBytes []byte) (*tls.Config, error)
}

// newTLSConfigAdapter returns a new TLSConfigGetter that updates the TLS config.
func newTLSConfigAdapter(cdnBaseURL string, mfLogger mfLogger, tlsConfigUpdater tlsConfigUpdater, log *slog.Logger) *tlsConfigAdapter {
	fetcher := contrastDeploymentFetcher{cdnBaseURL: cdnBaseURL}
	return &tlsConfigAdapter{
		fetcher:          fetcher,
		mfLogger:         mfLogger,
		tlsConfigUpdater: tlsConfigUpdater,
		log:              log,
	}
}

// GetTLSConfig retrieves the latest TLS config.
func (t tlsConfigAdapter) GetTLSConfig(ctx context.Context) (*tls.Config, error) {
	return getNewTLSConfig(ctx, t.fetcher, t.mfLogger, t.tlsConfigUpdater, t.log)
}

func getNewTLSConfig(ctx context.Context, fetcher contrastDeploymentFetcher, mfLogger mfLogger, tlsConfigUpdater tlsConfigUpdater, log *slog.Logger) (*tls.Config, error) {
	expectedMfBytes, err := fetcher.FetchManifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest: %w", err)
	}
	log.Info("Coordinator manifest fetched successfully")

	if err := mfLogger.Log(expectedMfBytes); err != nil {
		return nil, fmt.Errorf("log manifest: %w", err)
	}

	return tlsConfigUpdater.GetTLSConfig(ctx, expectedMfBytes)
}

type contrastDeploymentFetcher struct {
	cdnBaseURL string
}

func (f contrastDeploymentFetcher) FetchManifest(ctx context.Context) ([]byte, error) {
	return fetchBodyFromURL(ctx, fmt.Sprintf("%s/%s", f.cdnBaseURL, manifest))
}

type mfLogger struct {
	fs        afero.Fs
	workspace string
}

func (m mfLogger) Log(mf []byte) error {
	return manifestlog.WriteEntry(m.fs, m.workspace, mf)
}
