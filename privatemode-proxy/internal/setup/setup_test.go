// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package setup

import (
	"context"
	"crypto/x509"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avast/retry-go/v5"
	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager/updater"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/manifestlog"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestSecretManagerManifestLog(t *testing.T) {
	const manifest = `{"Policies":{"attested":{}}}`
	failure := errors.New("update failed")
	for _, stage := range []string{"success", "attestation failure", "exchange failure"} {
		t.Run(stage, func(t *testing.T) {
			require := require.New(t)
			fs := afero.NewMemMapFs()
			workspace := "workspace"
			manifestDir := filepath.Join(workspace, constants.ManifestDir)
			logger, err := manifestlog.New(fs, workspace)
			require.NoError(err)
			assertNoManifestLog := func() {
				files, err := afero.ReadDir(fs, manifestDir)
				require.NoError(err)
				require.Empty(files, "manifest and log must not be written before the secret update succeeds")
			}
			log := slog.New(slog.DiscardHandler)
			ca := &stubCAUpdater{check: assertNoManifestLog}
			client := &stubSecretClient{check: assertNoManifestLog}
			if stage == "attestation failure" {
				ca.err = retry.Unrecoverable(failure)
			}
			if stage == "exchange failure" {
				client.err = retry.Unrecoverable(failure)
			}
			adapter := &caAdapter{
				fetcher:   stubManifestFetcher{manifest: []byte(manifest)},
				caUpdater: ca,
				log:       log,
			}
			sm := newSecretManager(updater.New(client, adapter, log), adapter.CurrentManifest, logger, false)
			err = sm.OfferCredential(t.Context(), auth.APIKey("test-key"))
			if stage != "success" {
				require.ErrorIs(err, failure)
				assertNoManifestLog()
				return
			}
			require.NoError(err)
			secret, err := sm.LatestSecret(t.Context())
			require.NoError(err)
			require.Equal("exchanged-secret", secret.ID)
			require.Equal([]byte("secret-data"), secret.Data)
			manifestPath := filepath.Join(manifestDir, "1.json")
			content, err := afero.ReadFile(fs, manifestPath)
			require.NoError(err)
			require.Equal([]byte(manifest), content) //nolint:testifylint // Attestation requires byte-identical manifests.
			entries, err := afero.ReadFile(fs, filepath.Join(manifestDir, "log.txt"))
			require.NoError(err)
			require.Equal(1, strings.Count(string(entries), "\n"))
			require.True(strings.HasSuffix(string(entries), " "+manifestPath+" exchanged-secret\n"))
		})
	}
}

type stubManifestFetcher struct {
	manifest []byte
}

func (s stubManifestFetcher) FetchManifest(context.Context) ([]byte, error) {
	return s.manifest, nil
}

type stubCAUpdater struct {
	check func()
	err   error
}

func (s *stubCAUpdater) GetAttestedMeshCA(context.Context, []byte, auth.Credential) (*x509.Certificate, error) {
	s.check()
	return &x509.Certificate{}, s.err
}

type stubSecretClient struct {
	check func()
	err   error
}

func (s *stubSecretClient) ExchangeSecret(context.Context, *x509.Certificate, auth.Credential) (string, []byte, error) {
	s.check()
	return "exchanged-secret", []byte("secret-data"), s.err
}
