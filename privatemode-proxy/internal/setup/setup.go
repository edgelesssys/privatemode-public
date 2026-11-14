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
	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/httputil"
	"github.com/edgelesssys/continuum/internal/oss/secretclient"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager/updater"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/manifestlog"
	contrastsdk "github.com/edgelesssys/contrast/sdk"
	"github.com/spf13/afero"
)

const (
	// contrastSubDir is the subdirectory inside the workspace used to cache data related to the Contrast deployment.
	contrastSubDir = "contrast"
)

// SecretManager sets up the secret manager for the Contrast deployment.
func SecretManager(ctx context.Context, flags Flags, log *slog.Logger) (*secretmanager.SecretManager, func() string, error) {
	httpClient := httputil.NewClient()
	if flags.InsecureAPIConnection {
		httpClient = httputil.InsecureNewSkipVerifyClient()
	}

	contrastClient := contrastsdk.New().
		WithSlog(log.With("component", "contrast-client")).
		WithFSStore(afero.NewBasePathFs(afero.NewOsFs(), filepath.Join(flags.Workspace, contrastSubDir)))

	fs := afero.Afero{Fs: afero.NewOsFs()}
	extraHTTPHeaders := http.Header{constants.PrivatemodeClientHeader: {flags.ClientID}}
	ssClient := secretclient.New(httpClient, flags.APIEndpoint, extraHTTPHeaders)
	caUpdater := attest.NewGetter(httpClient, flags.APIEndpoint, contrastClient, extraHTTPHeaders)

	var caGetter updater.CAGetter
	var currentManifest func() string
	var mfLogger *manifestlog.Logger
	if flags.ManifestPath != "" { // static mode
		expectedMfBytes, err := fs.ReadFile(flags.ManifestPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read manifest file: %w", err)
		}
		caGetter = updater.NewStaticCAGetter(caUpdater, expectedMfBytes)
		currentManifest = func() string { return string(expectedMfBytes) }
	} else {
		caAdapter := newCAAdapter(flags.CDNBaseURL, caUpdater, log)
		caGetter = caAdapter
		currentManifest = caAdapter.CurrentManifest
		var err error
		mfLogger, err = manifestlog.New(fs, flags.Workspace)
		if err != nil {
			return nil, nil, fmt.Errorf("creating manifest logger: %w", err)
		}
	}

	secretUpdater := updater.New(ssClient, caGetter, log)
	credDropOnUnauthorized := flags.APIKey == nil

	sm := newSecretManager(secretUpdater, currentManifest, mfLogger, credDropOnUnauthorized)
	if flags.APIKey != nil {
		if err := sm.OfferCredential(ctx, auth.APIKey(*flags.APIKey)); err != nil {
			return nil, nil, fmt.Errorf("trying API key: %w", err)
		}
	}
	return sm, currentManifest, nil
}

func newSecretManager(secretUpdater *updater.Updater, currentManifest func() string, mfLogger *manifestlog.Logger, credDropOnUnauthorized bool) *secretmanager.SecretManager {
	return secretmanager.New(func(ctx context.Context, cred auth.Credential) (string, []byte, error) {
		id, data, err := secretUpdater.UpdateSecret(ctx, cred)
		if err != nil {
			return "", nil, err
		}
		if mfLogger != nil {
			if err := mfLogger.WriteEntry([]byte(currentManifest()), id); err != nil {
				return "", nil, fmt.Errorf("logging manifest: %w", err)
			}
		}
		return id, data, nil
	}, credDropOnUnauthorized)
}
