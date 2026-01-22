// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package middleware implements HTTP middleware shared across different modules.
package middleware

import (
	"bytes"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"path/filepath"
	"time"
)

// NewResponseRecorder returns a *ResponseRecorder that records the status code,
// any write errors, and the response body while still forwarding data to the
// underlying http.ResponseWriter.
func NewResponseRecorder(w http.ResponseWriter) *ResponseRecorder {
	return &ResponseRecorder{
		ResponseWriter: w,
		ResWriteErr:    nil,
		Status:         0,
		Body:           bytes.Buffer{},
	}
}

// ResponseRecorder records the HTTP response that is being written. It embeds
// http.ResponseWriter so it can be passed directly to handlers. Exported
// fields allow callers in other packages to inspect the recorded data.
type ResponseRecorder struct {
	http.ResponseWriter
	// ResWriteErr stores the first error encountered when writing to the client.
	ResWriteErr error
	// Status holds the HTTP status code written via WriteHeader.
	Status int
	// Body contains the full response payload as it was written.
	Body bytes.Buffer
}

// WriteHeader records the HTTP status code in the ResponseRecorder and
// forwards the call to the underlying http.ResponseWriter so the client
// receives the response with the correct status.
func (w *ResponseRecorder) WriteHeader(status int) {
	w.Status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *ResponseRecorder) Write(b []byte) (int, error) {
	// Only write to the remote client if we haven't already encountered an error.
	// The payload is always written to the in‑memory buffer.
	if w.ResWriteErr == nil {
		_, w.ResWriteErr = w.ResponseWriter.Write(b)
	}
	return w.Body.Write(b)
}

// Flush implements http.Flusher by delegating to the underlying ResponseWriter if it supports flushing.
func (w *ResponseRecorder) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// DumpRequestAndResponse is an HTTP middleware that writes the raw request to a file,
// then forwards the request to the next handler while capturing the response,
// and finally writes the captured response to a matching file in the given dumpDir.
func DumpRequestAndResponse(next http.Handler, logger *slog.Logger, dumpDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts := time.Now().UTC()
		dir := filepath.Join(dumpDir, ts.Format("2006-01-02"))
		base := fmt.Sprintf("%s_%02d", ts.Format("2006-01-02_150405.000000"), rand.Intn(100))
		reqPath := filepath.Join(dir, fmt.Sprintf("%s_req.txt", base))
		respPath := filepath.Join(dir, fmt.Sprintf("%s_resp.txt", base))

		if err := dumpRequestToFile(r, reqPath); err != nil {
			logger.Error("failed to dump request",
				"error", err,
				"path", r.URL.Path,
				"method", r.Method,
			)
		}

		rec := NewResponseRecorder(w)
		next.ServeHTTP(rec, r)

		if err := dumpResponseRecorderToFile(rec, respPath); err != nil {
			logger.Error("failed to dump response",
				"error", err,
				"status", rec.Status,
				"dumpDir", dumpDir,
			)
		}
	})
}
