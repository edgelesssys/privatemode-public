// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// package setup defines the build specific setup for the privatemode-proxy.
package setup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// secretLifetime is the lifetime of a secret.
	secretLifetime = 1 * time.Hour
	// secretRefreshBuffer controls how many minutes before expiration the secret should be refreshed.
	secretRefreshBuffer = 5 * time.Minute
)

// Flags are flags that are common to all setups.
type Flags struct {
	ContrastFlags
	Workspace      string
	ManifestPath   string
	SecretEndpoint string
}

// ContrastFlags holds the configuration for the Contrast deployment.
type ContrastFlags struct {
	CoordinatorEndpoint   string
	CoordinatorPolicyHash string
	CDNBaseURL            string
}

func fetchBodyFromURL(ctx context.Context, sourceURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return body, nil
}
