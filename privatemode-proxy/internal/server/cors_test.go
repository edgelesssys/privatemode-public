// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package server

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCORSAndMethods(t *testing.T) {
	const origin = "https://client.example"
	for name, tc := range map[string]struct {
		origins       []string
		method        string
		path          string
		origin        string
		requestMethod string
		status        int
		allowOrigin   string
		allow         string
	}{
		"disabled":             {nil, "OPTIONS", constants.OpenAIChatCompletionsEndpoint, origin, "POST", 405, "", "POST"},
		"allowed preflight":    {[]string{origin}, "OPTIONS", constants.OpenAIChatCompletionsEndpoint, origin, "POST", 204, origin, ""},
		"wildcard":             {[]string{"*"}, "OPTIONS", constants.AnthropicMessagesEndpoint, origin, "POST", 204, "*", ""},
		"disallowed preflight": {[]string{origin}, "OPTIONS", constants.OpenAIChatCompletionsEndpoint, "https://other.example", "POST", 405, "", "POST"},
		"disallowed origin":    {[]string{origin}, "GET", "/readyz", "https://other.example", "", 204, "", ""},
		"wrong chat method":    {[]string{origin}, "GET", constants.OpenAIChatCompletionsEndpoint, origin, "", 405, origin, "POST"},
		"wrong models method":  {[]string{origin}, "POST", constants.OpenAIModelsEndpoint, origin, "", 405, origin, "GET, HEAD"},
		"no origin":            {[]string{origin}, "GET", "/readyz", "", "", 204, "", ""},
		"unknown path":         {[]string{origin}, "POST", "/unknown", origin, "", 404, origin, ""},
		"wrong preflight":      {[]string{origin}, "OPTIONS", constants.OpenAIChatCompletionsEndpoint, origin, "DELETE", 405, origin, "POST"},
		"unknown preflight":    {[]string{origin}, "OPTIONS", "/unknown", origin, "POST", 404, origin, ""},
		"not a preflight":      {[]string{origin}, "OPTIONS", constants.OpenAIChatCompletionsEndpoint, origin, "", 405, origin, "POST"},
	} {
		t.Run(name, func(t *testing.T) {
			// A nil secret manager/forwarder catches accidental calls on these paths.
			s := &Server{log: slog.Default(), corsAllowedOrigins: tc.origins}
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Authorization", "Bearer unused-key")
			req.Header.Set("Access-Control-Request-Method", tc.requestMethod)
			req.Header.Set("Access-Control-Request-Headers", "authorization, content-type, x-stainless-lang")
			// Even oversized bodies must not be processed before method/CORS checks.
			req.ContentLength = constants.MaxFileSizeBytes + 1
			resp := httptest.NewRecorder()
			s.GetHandler().ServeHTTP(resp, req)
			assert.Equal(t, tc.status, resp.Code)
			assert.Equal(t, tc.allowOrigin, resp.Header().Get("Access-Control-Allow-Origin"))
			assert.Equal(t, tc.allow, resp.Header().Get("Allow"))
			assert.Empty(t, resp.Header().Get("Access-Control-Allow-Credentials"))
			assert.Contains(t, resp.Header().Values("Vary"), "Origin")
			if tc.allowOrigin != "" {
				assert.Equal(t, "*", resp.Header().Get("Access-Control-Expose-Headers"))
			} else {
				assert.Empty(t, resp.Header().Get("Access-Control-Expose-Headers"))
			}
			if tc.method == http.MethodOptions && tc.status == http.StatusNoContent {
				assert.Equal(t, tc.requestMethod, resp.Header().Get("Access-Control-Allow-Methods"))
				assert.Equal(t, req.Header.Get("Access-Control-Request-Headers"), resp.Header().Get("Access-Control-Allow-Headers"))
				assert.Equal(t, "86400", resp.Header().Get("Access-Control-Max-Age"))
				assert.Contains(t, resp.Header().Values("Vary"), "Access-Control-Request-Headers")
			} else {
				assert.Empty(t, resp.Header().Get("Access-Control-Max-Age"))
			}
		})
	}
}

func TestCORSUnmatchedPreflightUsesNext(t *testing.T) {
	const origin = "https://client.example"
	mux := http.NewServeMux()
	mux.HandleFunc("POST /known", func(http.ResponseWriter, *http.Request) {
		t.Fatal("preflight must not invoke the API handler")
	})
	for _, path := range []string{"/known", "/unknown"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, path, nil)
			req.Header.Set("Origin", origin)
			req.Header.Set("Access-Control-Request-Method", http.MethodDelete)
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				assert.Same(t, req, r)
				assert.Equal(t, http.MethodOptions, r.Method)
				w.Header().Set("X-Middleware", "visited")
			})
			resp := httptest.NewRecorder()
			cors(next, mux, []string{origin}).ServeHTTP(resp, req)
			assert.True(t, called)
			assert.Equal(t, "visited", resp.Header().Get("X-Middleware"))
			assert.Equal(t, origin, resp.Result().Header.Get("Access-Control-Allow-Origin"))
			assert.Empty(t, resp.Header().Get("Access-Control-Allow-Methods"))
			assert.Empty(t, resp.Header().Get("Access-Control-Max-Age"))
		})
	}
}

func TestCORSStreamingOverridesUpstream(t *testing.T) {
	const origin = "https://client.example"
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Accept-Encoding")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		assert.NoError(t, http.NewResponseController(w).Flush())
		<-release
	}))
	defer upstream.Close()
	defer close(release)

	for name, origins := range map[string][]string{"disabled": nil, "allowed origin": {origin}} {
		t.Run(name, func(t *testing.T) {
			s := New(upstream.Client(), &stubSecretManager{}, Opts{
				APIEndpoint: upstream.Listener.Addr().String(), ProtocolScheme: forwarder.SchemeHTTP,
				CORSAllowedOrigins: origins,
			}, slog.Default())
			proxy := httptest.NewServer(s.GetHandler())
			defer proxy.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.URL+constants.OpenAIModelsEndpoint, nil)
			require.NoError(t, err)
			req.Header.Set("Origin", origin)
			resp, err := proxy.Client().Do(req)
			require.NoError(t, err)
			line, err := bufio.NewReader(resp.Body).ReadString('\n')
			assert.NoError(t, err)
			assert.Equal(t, "data: first\n", line, "first event must arrive before upstream completes")
			for path, wantStatus := range map[string]int{constants.OpenAIModelsEndpoint: http.StatusOK, "/readyz": http.StatusNoContent} {
				probe, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.URL+path, nil)
				require.NoError(t, err)
				probe.Header.Set("Origin", origin)
				probeResp, err := proxy.Client().Do(probe)
				require.NoError(t, err)
				assert.Equal(t, wantStatus, probeResp.StatusCode)
				assert.NoError(t, probeResp.Body.Close())
				if len(origins) > 0 {
					assert.Equal(t, origin, probeResp.Header.Get("Access-Control-Allow-Origin"))
				}
			}
			assert.NoError(t, resp.Body.Close())
			cancel()
			assert.Empty(t, resp.Header.Get("Access-Control-Allow-Credentials"))
			if len(origins) == 0 {
				assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
			} else {
				assert.Equal(t, origin, resp.Header.Get("Access-Control-Allow-Origin"))
			}
			vary := strings.Join(resp.Header.Values("Vary"), ",")
			assert.Contains(t, vary, "Origin")
			assert.Contains(t, vary, "Accept-Encoding")
		})
	}
}
