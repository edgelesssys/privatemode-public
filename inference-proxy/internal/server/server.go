// Package server implements the user facing server that receives requests and forwards them to the appropriate inference API adapter.
package server

import (
	"context"
	"crypto/tls"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter"
	"github.com/edgelesssys/continuum/internal/oss/process"
)

// Server implements the user facing HTTP REST server.
type Server struct {
	adapter adapter.InferenceAdapter

	log *slog.Logger
}

// New creates a new Server.
func New(adapter adapter.InferenceAdapter, log *slog.Logger) *Server {
	return &Server{
		adapter: adapter,
		log:     log,
	}
}

// Serve starts the server.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	certs, err := tls.LoadX509KeyPair("/etc/tls/tls.crt", "/etc/tls/tls.key")
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:    listener.Addr().String(),
		Handler: s.adapter.ServeMux(),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{certs},
		},
		ErrorLog: newHTTPLogger(s.log), // Prometheus tries to scrape metrics from this TLS endpoint, causing errors we want to ignore
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
