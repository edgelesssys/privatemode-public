// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package server

import (
	"net/http"
	"slices"
	"strings"

	"github.com/edgelesssys/continuum/internal/oss/logging"
)

// cors handles preflight locally, before credential intake and body processing.
// It permits requested headers for allowed origins, but never cookie credentials.
func cors(next http.Handler, mux *http.ServeMux, origins []string) http.Handler {
	wildcard := slices.Contains(origins, "*")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Keep local CORS headers separate until the response is written, so forwarded
		// upstream headers cannot override them. Vary prevents reuse across origins.
		headers := http.Header{"Vary": {"Origin"}}
		cw := &corsResponseWriter{ResponseWriter: w, headers: headers}
		// Match exact origins or the wildcard; requests without Origin need no CORS.
		origin := r.Header.Get("Origin")
		allowed := origin != "" && (wildcard || slices.Contains(origins, origin))
		if allowed {
			if wildcard {
				origin = "*"
			}
			headers.Set("Access-Control-Allow-Origin", origin)
			headers.Set("Access-Control-Expose-Headers", "*")
		}

		// For preflight, check the requested method against the mux without invoking
		// an API handler. Allow its requested headers only if the route exists.
		if allowed && r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			headers.Add("Vary", "Access-Control-Request-Method")
			headers.Add("Vary", "Access-Control-Request-Headers")
			probe := *r // shallow copy suffices: the mux only reads Method, URL, and Host
			probe.Method = r.Header.Get("Access-Control-Request-Method")
			_, pattern := mux.Handler(&probe)
			if pattern == "" {
				// Unmatched preflights still use the normal middleware stack.
				next.ServeHTTP(cw, r)
				cw.WriteHeader(http.StatusOK)
				return
			}
			logging.AddSummaryAttrs(r.Context(), "httpRoute", pattern)
			headers.Set("Access-Control-Allow-Methods", probe.Method)
			headers.Set("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
			headers.Set("Access-Control-Max-Age", "86400")
			cw.WriteHeader(http.StatusNoContent)
			return
		}
		// Other requests use the normal handler chain, including disallowed origins
		// without CORS permission headers. Commit headers even if the handler writes nothing.
		next.ServeHTTP(cw, r)
		cw.WriteHeader(http.StatusOK)
	})
}

// The local policy is authoritative, including when CORS is disabled. Upstream
// response headers must not broaden it or enable credentialed browser requests.
// A writer wrapper is needed because the forwarder copies upstream headers just
// before writing: setting headers before next.ServeHTTP would be overwritten,
// and setting them afterwards would be too late. Flush preserves SSE streaming.
type corsResponseWriter struct {
	http.ResponseWriter
	headers     http.Header
	wroteHeader bool
}

func (w *corsResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	for key := range w.Header() {
		if strings.HasPrefix(key, "Access-Control-") {
			delete(w.Header(), key)
		}
	}
	for key, values := range w.headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *corsResponseWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.ResponseWriter.Write(p)
}

func (w *corsResponseWriter) Flush() {
	w.WriteHeader(http.StatusOK)
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *corsResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
