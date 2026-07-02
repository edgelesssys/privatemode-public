// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package middleware

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/logging"
)

// LogRequestSummary is a middleware writing a "Served request" summary log line per request. Nested
// middlewares and handlers can add fields via [logging.AddSummaryAttrs]. If logStart is true,
// a "Serving request" line is also emitted at entry (with fewer fields).
func LogRequestSummary(next http.Handler, log *slog.Logger, logStart bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, attrs := logging.WithSummaryAttrs(r.Context())
		r = r.WithContext(ctx)

		ip, portStr, _ := net.SplitHostPort(r.RemoteAddr)
		port, _ := strconv.Atoi(portStr)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		args := []any{
			slog.String("method", r.Method),
			slog.String("scheme", scheme),
			// pm-proxy < v1.47 sends its own hostname/IP towards Privatemode infrastructure, which
			// we don't want to log.
			// slog.String("host", r.Host),
			slog.String("path", r.URL.Path),
			slog.String("query", r.URL.RawQuery),
			slog.String("protocol", r.Proto),
			slog.String("userAgent", r.UserAgent()),
			slog.String("ip", ip),
			slog.Int("port", port),
		}

		if logStart {
			log.InfoContext(ctx, "Serving request", args...)
		}

		cr := &countingReader{rc: r.Body}
		if r.Body != nil {
			r.Body = cr
		}
		cw := &countingResponseWriter{ResponseWriter: w}

		start := time.Now()
		next.ServeHTTP(cw, r)
		duration := time.Since(start)

		status := cw.status
		if status == 0 {
			status = http.StatusOK
		}

		outcome := "success"
		if status >= http.StatusInternalServerError {
			outcome = "failure"
		}

		args = append(
			args,
			slog.Int("httpStatus", status),
			slog.String("outcome", outcome),
			slog.Duration("duration", duration),
			slog.Int64("requestSize", cr.n),
			slog.Int64("responseSize", cw.n),
		)
		args = append(args, attrs.Args()...)
		log.InfoContext(ctx, "Served request", args...)
	})
}

// countingReader wraps an [io.ReadCloser] to count the bytes read from it.
type countingReader struct {
	rc io.ReadCloser
	n  int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.rc.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *countingReader) Close() error {
	return c.rc.Close()
}

// countingResponseWriter wraps a [http.ResponseWriter] to record the status code and count bytes
// written.
type countingResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	n           int64
}

func (w *countingResponseWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *countingResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		// Mirror net/http: a Write without an explicit WriteHeader implies 200.
		w.status = http.StatusOK
		w.wroteHeader = true
	}
	n, err := w.ResponseWriter.Write(b)
	w.n += int64(n)
	return n, err
}

// Unwrap exposes the wrapped writer for compatibility with [http.NewResponseController].
func (w *countingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
