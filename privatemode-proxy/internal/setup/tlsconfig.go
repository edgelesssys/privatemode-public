// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package setup

import (
	"context"
	"crypto/x509"
	"fmt"
	"log/slog"
	"sync"

	"github.com/edgelesssys/continuum/privatemode-proxy/internal/manifestlog"
	"github.com/spf13/afero"
)

const manifest = "manifest.json"

// caAdapter updates the mesh CA with the interface for the secretupdater.
type caAdapter struct {
	fetcher   contrastDeploymentFetcher
	mfLogger  mfLogger
	caUpdater caUpdater
	log       *slog.Logger

	manifestMu sync.Mutex
	manifest   []byte
}

type caUpdater interface {
	GetAttestedMeshCA(ctx context.Context, expectedMfBytes []byte, apiKey string) (*x509.Certificate, error)
}

// newCAAdapter creates a new caAdapter.
func newCAAdapter(cdnBaseURL string, mfLogger mfLogger, caUpdater caUpdater, log *slog.Logger) *caAdapter {
	fetcher := contrastDeploymentFetcher{cdnBaseURL: cdnBaseURL}
	return &caAdapter{
		fetcher:   fetcher,
		mfLogger:  mfLogger,
		caUpdater: caUpdater,
		log:       log,
	}
}

// GetMeshCA retrieves the latest manifest and gets the attested mesh CA.
func (c *caAdapter) GetMeshCA(ctx context.Context, apiKey string) (*x509.Certificate, error) {
	expectedMfBytes, err := c.fetcher.FetchManifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest: %w", err)
	}
	c.log.Info("Coordinator manifest fetched successfully")

	if err := c.mfLogger.Log(expectedMfBytes); err != nil {
		return nil, fmt.Errorf("logging manifest: %w", err)
	}

	cert, err := c.caUpdater.GetAttestedMeshCA(ctx, expectedMfBytes, apiKey)
	if err != nil {
		return nil, fmt.Errorf("getting attested certificate: %w", err)
	}

	c.manifestMu.Lock()
	c.manifest = expectedMfBytes
	c.manifestMu.Unlock()

	return cert, nil
}

func (c *caAdapter) CurrentManifest() string {
	c.manifestMu.Lock()
	defer c.manifestMu.Unlock()
	return string(c.manifest)
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
