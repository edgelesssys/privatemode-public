// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package tlsconfig contains logic for updating the TLS config for the Contrast client.
package tlsconfig

import (
	"context"
	"crypto/tls"
	"fmt"
	"path/filepath"

	"github.com/edgelesssys/continuum/internal/oss/contrast"
	contrastsdk "github.com/edgelesssys/contrast/sdk"
)

const (
	// ContrastSubDir is the subdirectory inside the workspace used to cache data related to the Contrast deployment.
	ContrastSubDir = "contrast"
)

// Getter is a client that gets the TLS config for the Contrast deployment.
type Getter struct {
	contrastClient      contrastsdk.Client
	coordinatorEndpoint string
	workspaceDir        string
}

// NewGetter returns a new TLSConfigGetter that gets the Contrast TLS config.
func NewGetter(coordinatorEndpoint string, contrastClient contrastsdk.Client, workspaceDir string) Getter {
	return Getter{
		contrastClient:      contrastClient,
		coordinatorEndpoint: coordinatorEndpoint,
		workspaceDir:        workspaceDir,
	}
}

// GetTLSConfig gets the TLS config for the Contrast deployment.
func (c Getter) GetTLSConfig(ctx context.Context, expectedMfBytes []byte) (*tls.Config, error) {
	cacheDir := filepath.Join(c.workspaceDir, ContrastSubDir)
	state, err := fetchAndVerifyCoordinatorState(ctx, cacheDir, c.coordinatorEndpoint, c.contrastClient, expectedMfBytes)
	if err != nil {
		return nil, fmt.Errorf("getting coordinator state: %w", err)
	}

	tlsConfig, err := contrast.ClientTLSConfig(state.MeshCA, nil)
	if err != nil {
		return nil, fmt.Errorf("loading TLS config: %w", err)
	}
	return tlsConfig, nil
}

func fetchAndVerifyCoordinatorState(ctx context.Context, cacheDir string, coordinatorEndpoint string, cclient contrastsdk.Client, expectedMfBytes []byte) (contrastsdk.CoordinatorState, error) {
	state, err := cclient.GetCoordinatorState(ctx, cacheDir, expectedMfBytes, coordinatorEndpoint)
	if err != nil {
		return contrastsdk.CoordinatorState{}, fmt.Errorf("getting coordinator state: %w", err)
	}
	if err := cclient.Verify(expectedMfBytes, state.Manifests); err != nil {
		return contrastsdk.CoordinatorState{}, fmt.Errorf("verifying Contrast manifest: %w", err)
	}
	return state, err
}
