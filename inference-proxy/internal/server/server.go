// Package server implements the user facing server that receives requests and forwards them to the appropriate inference API adapter.
package server

import (
	"context"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter"
	"github.com/edgelesssys/continuum/internal/mtls"
	"github.com/edgelesssys/continuum/internal/oss/process"
)

// Server implements the user facing HTTP REST server.
type Server struct {
	adapters     []adapter.InferenceAdapter
	mtlsIdentity mtls.Identity
	log          *slog.Logger
}

// New creates a new Server.
func New(adapters []adapter.InferenceAdapter, mtlsIdentity mtls.Identity, log *slog.Logger) *Server {
	return &Server{
		adapters:     adapters,
		mtlsIdentity: mtlsIdentity,
		log:          log,
	}
}

// Serve starts the server.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	// Build combined ServeMux from all adapters.
	// Each adapter registers its routes with middleware already applied per-route.
	mux := http.NewServeMux()

	// Check if any adapter handles catch-all routing (e.g., unstructured, unencrypted).
	// If so, skip registering the server-level 501 handler to avoid conflicts.
	hasCatchAll := false
	for _, a := range s.adapters {
		if a.HandlesCatchAll() {
			hasCatchAll = true
			break
		}
	}
	if !hasCatchAll {
		mux.HandleFunc("/", adapter.UnsupportedEndpoint)
	}

	for _, a := range s.adapters {
		a.RegisterRoutes(mux)
	}

	server := &http.Server{
		Addr:      listener.Addr().String(),
		Handler:   mux,
		TLSConfig: s.mtlsIdentity.ServerConfig(),
		ErrorLog:  newHTTPLogger(s.log), // Prometheus tries to scrape metrics from this TLS endpoint, causing errors we want to ignore
	}
	return process.HTTPServeContext(ctx, server, listener, s.log)
}

type httpLogger struct {
	infoLog  *log.Logger
	errorLog *log.Logger
}

func newHTTPLogger(slogger *slog.Logger) *log.Logger {
	return log.New(&httpLogger{
		infoLog:  slog.NewLogLogger(slogger.With("component", "httpErrorLog").Handler(), slog.LevelInfo),
		errorLog: slog.NewLogLogger(slogger.With("component", "httpErrorLog").Handler(), slog.LevelError),
	}, "", 0)
}

func (h *httpLogger) Write(b []byte) (n int, err error) {
	s := string(b)
	if strings.HasPrefix(s, "http: TLS handshake error from") {
		h.infoLog.Print(s)
	} else {
		h.errorLog.Print(s)
	}
	return len(b), nil
}
