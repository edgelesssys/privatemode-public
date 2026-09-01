// Package health implements the secret-service health check server.
package health

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"

	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/process"
)

// Server handles health check requests.
type Server struct {
	log     *slog.Logger
	healthy *atomic.Bool
}

// New initializes a new Server.
func New(log *slog.Logger) *Server {
	healthy := &atomic.Bool{}
	healthy.Store(true)
	return &Server{
		log:     log,
		healthy: healthy,
	}
}

// Serve starts the health server on the given endpoint.
func (s *Server) Serve(ctx context.Context, endpoint string) error {
	lis, err := net.Listen("tcp", endpoint)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	server := &http.Server{
		Addr:     endpoint,
		ErrorLog: logging.NewHTTPErrorLogger(s.log),
		Handler:  http.HandlerFunc(s.handler),
	}
	return process.HTTPServeContext(ctx, server, lis, s.log)
}

// handler is the HTTP health handler.
func (s *Server) handler(w http.ResponseWriter, _ *http.Request) {
	if !s.healthy.Load() {
		http.Error(w, "NOT OK", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("OK"))
}

// SetHealthy sets the health status of the server.
func (s *Server) SetHealthy(healthy bool) {
	s.healthy.Store(healthy)
}
