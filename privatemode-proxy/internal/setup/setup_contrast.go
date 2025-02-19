// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

//go:build contrast_unstable_api

package setup

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/edgelesssys/continuum/internal/gpl/contrast/client"
	"github.com/edgelesssys/continuum/internal/gpl/secretmanager"
	"github.com/edgelesssys/continuum/internal/gpl/secretmanager/updater"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/setup/tlsconfig"
	contrastsdk "github.com/edgelesssys/contrast/sdk"
	"github.com/spf13/afero"
)

// SecretManager sets up the secret manager for the Contrast deployment.
func SecretManager(ctx context.Context, flags Flags, log *slog.Logger) (*secretmanager.SecretManager, error) {
	tlsConfigGetter := tlsconfig.NewGetter(flags.CoordinatorEndpoint, contrastsdk.NewWithSlog(log.With("component", "contrast-client")), flags.Workspace)
	if flags.ManifestPath != "" { // static mode
		if flags.CoordinatorPolicyHash == "" {
			return nil, fmt.Errorf("coordinatorPolicyHash is required in static mode")
		}
		fs := afero.Afero{Fs: afero.NewOsFs()}
		expectedMfBytes, err := fs.ReadFile(flags.ManifestPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read manifest file: %w", err)
		}
		tlsConfig, err := tlsConfigGetter.GetTLSConfig(ctx, expectedMfBytes, flags.CoordinatorPolicyHash)
		if err != nil {
			return nil, fmt.Errorf("updating TLS config: %w", err)
		}
		ssClient := client.New(flags.SecretEndpoint, tlsConfig, &client.Opts{Log: log, RetryInterval: 0, MaxRetries: 0})
		secretUpdater := updater.New(ssClient, updater.NewStaticTLSConfigGetter(tlsConfig), log)
		sm := secretmanager.New(secretUpdater.UpdateSecrets,
			secretLifetime, secretRefreshBuffer,
		)
		return sm, nil
	}
	fs := afero.Afero{Fs: afero.NewOsFs()}
	tlsConfigAdapter := newTLSConfigAdapter(flags.CDNBaseURL, mfLogger{fs: fs, workspace: flags.Workspace}, tlsConfigGetter, log)
	tlsConfig, err := tlsConfigAdapter.GetTLSConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting TLS config: %w", err)
	}
	ssClient := client.New(flags.SecretEndpoint, tlsConfig, &client.Opts{Log: log, RetryInterval: 0, MaxRetries: 0})
	secretUpdater := updater.New(ssClient, tlsConfigAdapter, log)
	sm := secretmanager.New(secretUpdater.UpdateSecrets,
		secretLifetime, secretRefreshBuffer,
	)
	return sm, nil
}
