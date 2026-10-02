// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewModelsDocument(t *testing.T) {
	testCases := map[string]struct {
		content *string
		wantErr bool
	}{
		"valid":        {content: new(`{"data":[]}`)},
		"invalid json": {content: new(`{"data":`), wantErr: true},
		"wrong shape":  {content: new(`{"object":"list"}`), wantErr: true},
		"missing file": {wantErr: true},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "models.json")
			if tc.content != nil {
				require.NoError(t, os.WriteFile(path, []byte(*tc.content), 0o600))
			}

			_, err := NewModelsDocument(path, slog.Default())
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestModelsDocumentServeHTTP(t *testing.T) {
	const first = `{"data":[{"id":"first"}]}`
	const second = `{"data":[{"id":"second"}]}`
	const sameSize = `{"data":[{"id":"FIRST"}]}` // same length as first

	testCases := map[string]struct {
		update    *string // written to the file after construction, "" deletes it
		keepMtime bool    // restore the original modification time after the update
		wantBody  string
	}{
		"initial document":             {wantBody: first},
		"edit is picked up":            {update: new(second), wantBody: second},
		"same size edit is picked up":  {update: new(sameSize), wantBody: sameSize},
		"unchanged stat is not reread": {update: new(sameSize), keepMtime: true, wantBody: first},
		"invalid edit keeps last good": {update: new(`{"data":`), wantBody: first},
		"wrong shape keeps last good":  {update: new(`{"object":"list"}`), wantBody: first},
		"deleted file keeps last good": {update: new(""), wantBody: first},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			path := filepath.Join(t.TempDir(), "models.json")
			require.NoError(os.WriteFile(path, []byte(first), 0o600))
			info, err := os.Stat(path)
			require.NoError(err)
			doc, err := NewModelsDocument(path, slog.Default())
			require.NoError(err)

			if tc.update != nil {
				if *tc.update == "" {
					require.NoError(os.Remove(path))
				} else {
					require.NoError(os.WriteFile(path, []byte(*tc.update), 0o600))
					// Don't depend on the file system's timestamp resolution.
					mtime := info.ModTime().Add(time.Second)
					if tc.keepMtime {
						mtime = info.ModTime()
					}
					require.NoError(os.Chtimes(path, mtime, mtime))
				}
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, constants.OpenAIModelsEndpoint, http.NoBody)
			resp := httptest.NewRecorder()
			doc.ServeHTTP(resp, req)

			assert.Equal(http.StatusOK, resp.Code)
			assert.Equal("application/json", resp.Header().Get("Content-Type"))
			assert.Equal(tc.wantBody, resp.Body.String())
		})
	}
}

func TestModelsEndpointRouting(t *testing.T) {
	const doc = `{"data":[{"id":"static"}]}`
	const forwarded = `{"data":[{"id":"forwarded"}]}`

	testCases := map[string]struct {
		withDocument bool
		wantBody     string
	}{
		"document configured":                           {withDocument: true, wantBody: doc},
		"document not configured, request is forwarded": {wantBody: forwarded},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(forwarded))
			}))
			defer backend.Close()

			secret := secretmanager.Secret{ID: "123", Data: bytes.Repeat([]byte{0x42}, 32)}
			sut := newTestServer(nil, secret, backend.Listener.Addr().String(), "", false)
			if tc.withDocument {
				path := filepath.Join(t.TempDir(), "models.json")
				require.NoError(os.WriteFile(path, []byte(doc), 0o600))
				var err error
				sut.modelsDocument, err = NewModelsDocument(path, slog.Default())
				require.NoError(err)
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, constants.OpenAIModelsEndpoint, http.NoBody)
			resp := httptest.NewRecorder()
			sut.GetHandler().ServeHTTP(resp, req)

			assert.Equal(http.StatusOK, resp.Code)
			assert.Equal(tc.wantBody, resp.Body.String())
		})
	}
}
