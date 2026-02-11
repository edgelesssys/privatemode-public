// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package attest provides a Getter to get the mesh CA verified via remote attestation.
package attest

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/edgelesssys/continuum/internal/oss/httpapi"
	contrastsdk "github.com/edgelesssys/contrast/sdk"
)

const (
	// ContrastSubDir is the subdirectory inside the workspace used to cache data related to the Contrast deployment.
	ContrastSubDir = "contrast"
)

// Getter is a client that gets the mesh CA of the Privatemode deployment.
type Getter struct {
	httpClient     *http.Client
	contrastClient contrastsdk.Client
	endpoint       string
	workspaceDir   string
}

// NewGetter creates a new Getter.
func NewGetter(httpClient *http.Client, endpoint string, contrastClient contrastsdk.Client, workspaceDir string) Getter {
	return Getter{
		httpClient:     httpClient,
		contrastClient: contrastClient,
		endpoint:       endpoint,
		workspaceDir:   workspaceDir,
	}
}

// GetAttestedMeshCA gets the mesh CA of the Privatemode deployment.
func (c Getter) GetAttestedMeshCA(ctx context.Context, expectedMfBytes []byte, apiKey string) (*x509.Certificate, error) {
	// Get the attestation
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	attestReq, err := json.Marshal(httpapi.AttestReq{Nonce: nonce})
	if err != nil {
		return nil, fmt.Errorf("marshaling attest request: %w", err)
	}
	attestResp, err := httpapi.Do(ctx, c.httpClient, http.MethodPost, "https://"+c.endpoint+"/privatemode/v1/attest", attestReq, apiKey)
	if err != nil {
		return nil, fmt.Errorf("doing attest request: %w", err)
	}
	var att httpapi.AttestResp
	if err := json.Unmarshal(attestResp, &att); err != nil {
		return nil, fmt.Errorf("unmarshaling attest response: %w", err)
	}

	// Validate the attestation against the expected manifest
	coordinatorState, err := c.contrastClient.ValidateAttestation(ctx, filepath.Join(c.workspaceDir, ContrastSubDir), nonce, att.AttestationDoc)
	if err != nil {
		return nil, fmt.Errorf("validating attestation: %w", err)
	}
	if len(coordinatorState.Manifests) != 1 {
		return nil, errors.New("expected exactly one manifest")
	}
	if !bytes.Equal(coordinatorState.Manifests[0], expectedMfBytes) {
		return nil, errors.New("active manifest does not match expected manifest")
	}

	// Parse the certificate
	block, _ := pem.Decode(coordinatorState.MeshCA)
	if block == nil {
		return nil, errors.New("decoding mesh CA certificate failed")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing mesh CA certificate: %w", err)
	}

	return cert, nil
}
