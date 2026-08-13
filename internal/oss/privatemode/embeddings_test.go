// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package privatemode

import (
	"io"
	"net/http"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareEmbeddingsRequest(t *testing.T) {
	testCases := map[string]struct {
		opts RequestOptions
	}{
		"without additional headers": {},
		"with additional headers": {
			opts: RequestOptions{Header: http.Header{"X-Custom-Header": []string{"custom-value"}}},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			client := New(auth.APIKey("test-key")).WithAPIBaseURL("https://example.com")
			body := []byte(`{"model":"qwen3-embedding-4b","input":["hello","world"],"dimensions":1024}`)

			req, err := client.prepareEmbeddingsRequest(t.Context(), body, tc.opts)
			require.NoError(t, err)

			assert.Equal(t, http.MethodPost, req.Method)
			assert.Equal(t, "https://example.com"+openai.EmbeddingsEndpoint, req.URL.String())
			assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
			assert.Equal(t, "qwen3-embedding-4b", req.Header.Get(constants.PrivatemodeTargetModel))
			assert.Equal(t, tc.opts.Header.Get("X-Custom-Header"), req.Header.Get("X-Custom-Header"))
			gotBody, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			assert.JSONEq(t, string(body), string(gotBody))
		})
	}
}
