// Package server implements the user facing server that receives requests and forwards them to the appropriate inference API adapter.
package server

import (
	"log/slog"
	"net"
	"net/http"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter"
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
func (s *Server) Serve(listener net.Listener) error {
	return http.ServeTLS(listener, s.adapter.ServeMux(), "/etc/tls/tls.crt", "/etc/tls/tls.key")
}

// serveInsecure starts the server without TLS. Only used in testing.
func (s *Server) serveInsecure(listener net.Listener) error {
	return http.Serve(listener, s.adapter.ServeMux())
}
