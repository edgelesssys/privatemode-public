// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package middleware

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummaryServedLog(t *testing.T) {
	respBody := "hello world response"
	reqBody := strings.Repeat("x", 1234)
	chunk := "data: chunk\n\n"
	chunkCount := 5

	testCases := map[string]struct {
		handler http.HandlerFunc
		req     *http.Request
		assert  func(t *testing.T, a map[string]any)
	}{
		"unary records all fields and body sizes": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				w.WriteHeader(http.StatusCreated)
				_, err = io.WriteString(w, respBody)
				assert.NoError(t, err)
			},
			req: func() *http.Request {
				req := httptest.NewRequestWithContext(
					t.Context(),
					http.MethodPost,
					constants.OpenAIChatCompletionsEndpoint+"?foo=bar",
					strings.NewReader(reqBody),
				)
				req.RemoteAddr = "10.0.0.5:54321"
				return req
			}(),
			assert: func(t *testing.T, a map[string]any) {
				assert.Equal(t, http.MethodPost, a["method"])
				assert.Equal(t, "http", a["scheme"])
				assert.Empty(t, a["host"]) // Temporarily no logging of `Host` header
				assert.Equal(t, constants.OpenAIChatCompletionsEndpoint, a["path"])
				assert.Equal(t, "foo=bar", a["query"])
				assert.Equal(t, "HTTP/1.1", a["protocol"])
				assert.Equal(t, "10.0.0.5", a["ip"])
				assert.EqualValues(t, 54321, a["port"])
				assert.EqualValues(t, http.StatusCreated, a["httpStatus"])
				assert.Equal(t, "success", a["outcome"])
				assert.EqualValues(t, len(reqBody), a["requestSize"])
				assert.EqualValues(t, len(respBody), a["responseSize"])
			},
		},
		"streaming counts flushed bytes and defaults status to 200": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				rc := http.NewResponseController(w)
				for range chunkCount {
					_, err := io.WriteString(w, chunk)
					assert.NoError(t, err)
					// Flush must succeed through the counting wrapper via Unwrap.
					assert.NoError(t, rc.Flush())
				}
			},
			req: httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/stream", http.NoBody),
			assert: func(t *testing.T, a map[string]any) {
				assert.EqualValues(t, len(chunk)*chunkCount, a["responseSize"])
				assert.EqualValues(t, http.StatusOK, a["httpStatus"], "implicit status should default to 200")
			},
		},
		"server error maps to failure outcome": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
			req: httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody),
			assert: func(t *testing.T, a map[string]any) {
				assert.EqualValues(t, http.StatusBadGateway, a["httpStatus"])
				assert.Equal(t, "failure", a["outcome"])
			},
		},
		"attrs added via context appear on the log": {
			handler: func(_ http.ResponseWriter, r *http.Request) {
				logging.AddSummaryAttrs(r.Context(), "pattern", "POST "+constants.OpenAIChatCompletionsEndpoint, "apiKey", "secret")
			},
			req: httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody),
			assert: func(t *testing.T, a map[string]any) {
				assert.Equal(t, "POST "+constants.OpenAIChatCompletionsEndpoint, a["pattern"])
				assert.Equal(t, "secret", a["apiKey"])
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			rec := &recordingHandler{}
			handler := LogRequestSummary(tc.handler, slog.New(rec), false)
			handler.ServeHTTP(httptest.NewRecorder(), tc.req)

			require.Len(t, rec.records, 1)
			assert.Equal(t, "Served request", rec.records[0].Message)
			tc.assert(t, recordToAttrMap(rec.records[0]))
		})
	}
}

func TestSummaryServedLogOnPanic(t *testing.T) {
	rec := &recordingHandler{}
	handler := LogRequestSummary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		logging.AddSummaryAttrs(r.Context(), "aborted", true)
		panic(http.ErrAbortHandler)
	}), slog.New(rec), false)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, constants.OpenAIChatCompletionsEndpoint, http.NoBody)

	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		handler.ServeHTTP(httptest.NewRecorder(), req)
	})

	require.Len(t, rec.records, 1)
	assert.Equal(t, "Served request", rec.records[0].Message)
	a := recordToAttrMap(rec.records[0])
	assert.EqualValues(t, http.StatusOK, a["httpStatus"])
	assert.Equal(t, true, a["aborted"])
}

func TestSummaryStartLog(t *testing.T) {
	rec := &recordingHandler{}
	handler := LogRequestSummary(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), slog.New(rec), true)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.Len(t, rec.records, 2)
	assert.Equal(t, "Serving request", rec.records[0].Message)
	assert.Equal(t, "Served request", rec.records[1].Message)
}

// recordingHandler is a slog.Handler that captures emitted records for assertions.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func recordToAttrMap(r slog.Record) map[string]any {
	m := make(map[string]any, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	return m
}
