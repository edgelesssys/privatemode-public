// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// Package tlsconfig contains logic for updating the TLS config for the Contrast client.
package tlsconfig

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/edgelesssys/continuum/internal/gpl/contrast"
	contrastsdk "github.com/edgelesssys/contrast/sdk"
)

const (
	// ContrastSubDir is the subdirectory inside the workspace used to cache data related to the Contrast deployment.
	ContrastSubDir = "contrast"
)

// Getter is a client that gets the TLS config for the Contrast deployment.
type Getter struct {
	contrastClient      contrastClient
	coordinatorEndpoint string
	workspaceDir        string
}

type contrastClient interface {
	GetCoordinatorState(ctx context.Context, cacheDir string, expectedMfBytes []byte, endpoint string, policyHash []byte) (contrastsdk.CoordinatorState, error)
	Verify(expectedMfBytes []byte, manifests [][]byte) error
}

// NewGetter returns a new TLSConfigGetter that gets the Contrast TLS config.
func NewGetter(coordinatorEndpoint string, contrastClient contrastClient, workspaceDir string) Getter {
	return Getter{
		contrastClient:      contrastClient,
		coordinatorEndpoint: coordinatorEndpoint,
		workspaceDir:        workspaceDir,
	}
}

// GetTLSConfig gets the TLS config for the Contrast deployment.
func (c Getter) GetTLSConfig(ctx context.Context, expectedMfBytes []byte, coordinatorPolicyHash string) (*tls.Config, error) {
	cacheDir := filepath.Join(c.workspaceDir, ContrastSubDir)
	state, err := fetchAndVerifyCoordinatorState(ctx, cacheDir, c.coordinatorEndpoint, c.contrastClient, expectedMfBytes, coordinatorPolicyHash)
	if err != nil {
		return nil, fmt.Errorf("getting coordinator state: %w", err)
	}

	tlsConfig, err := contrast.ClientTLSConfig(state.MeshCA, nil)
	if err != nil {
		return nil, fmt.Errorf("loading TLS config: %w", err)
	}
	return tlsConfig, nil
}

func fetchAndVerifyCoordinatorState(ctx context.Context, cacheDir string, coordinatorEndpoint string, cclient contrastClient, expectedMfBytes []byte, hexCoordinatorPolicyHash string) (contrastsdk.CoordinatorState, error) {
	policyHash, err := decodeCoordinatorPolicyHash(hexCoordinatorPolicyHash)
	if err != nil {
		return contrastsdk.CoordinatorState{}, fmt.Errorf("decoding coordinator policy hash: %w", err)
	}
	state, err := cclient.GetCoordinatorState(ctx, cacheDir, expectedMfBytes, coordinatorEndpoint, policyHash)
	if err != nil {
		return contrastsdk.CoordinatorState{}, fmt.Errorf("getting coordinator state: %w", err)
	}
	if err := cclient.Verify(expectedMfBytes, state.Manifests); err != nil {
		return contrastsdk.CoordinatorState{}, fmt.Errorf("verifying Contrast manifest: %w", err)
	}
	return state, err
}

func decodeCoordinatorPolicyHash(hexEncoded string) ([]byte, error) {
	hash, err := hex.DecodeString(hexEncoded)
	if err != nil {
		return nil, fmt.Errorf("hex-decoding coordinator-policy-hash flag: %w", err)
	}
	if len(hash) != 32 {
		return nil, fmt.Errorf("coordinator-policy-hash must be exactly 32 hex-encoded bytes, got %d", len(hash))
	}
	return hash, nil
}
