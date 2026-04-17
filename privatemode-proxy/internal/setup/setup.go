// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package setup

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/edgelesssys/continuum/internal/oss/attest"
	"github.com/edgelesssys/continuum/internal/oss/httputil"
	"github.com/edgelesssys/continuum/internal/oss/secretclient"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager/updater"
	contrastsdk "github.com/edgelesssys/contrast/sdk"
	"github.com/spf13/afero"
)

const (
	// contrastSubDir is the subdirectory inside the workspace used to cache data related to the Contrast deployment.
	contrastSubDir = "contrast"
)

// SecretManager sets up the secret manager for the Contrast deployment.
func SecretManager(ctx context.Context, flags Flags, log *slog.Logger) (*secretmanager.SecretManager, func() string, error) {
	httpClient := http.DefaultClient
	if flags.InsecureAPIConnection {
		httpClient = httputil.InsecureNewSkipVerifyClient()
	}

	contrastClient := contrastsdk.New().
		WithSlog(log.With("component", "contrast-client")).
		WithFSStore(afero.NewBasePathFs(afero.NewOsFs(), filepath.Join(flags.Workspace, contrastSubDir)))

	fs := afero.Afero{Fs: afero.NewOsFs()}
	ssClient := secretclient.New(httpClient, flags.APIEndpoint)
	caUpdater := attest.NewGetter(httpClient, flags.APIEndpoint, contrastClient)

	var caGetter updater.CAGetter
	var currentManifest func() string
	if flags.ManifestPath != "" { // static mode
		expectedMfBytes, err := fs.ReadFile(flags.ManifestPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read manifest file: %w", err)
		}
		caGetter = updater.NewStaticCAGetter(caUpdater, expectedMfBytes)
		currentManifest = func() string { return string(expectedMfBytes) }
	} else {
		caAdapter := newCAAdapter(flags.CDNBaseURL, mfLogger{fs: fs, workspace: flags.Workspace}, caUpdater, log)
		caGetter = caAdapter
		currentManifest = caAdapter.CurrentManifest
	}

	secretUpdater := updater.New(ssClient, caGetter, log)
	apiKeyDropOnUnauthorized := flags.APIKey == nil
	sm := secretmanager.New(secretUpdater.UpdateSecret, apiKeyDropOnUnauthorized)
	if flags.APIKey != nil {
		if err := sm.OfferAPIKey(ctx, *flags.APIKey); err != nil {
			return nil, nil, fmt.Errorf("trying API key: %w", err)
		}
	}
	return sm, currentManifest, nil
}
