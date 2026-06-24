package health

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"

	"github.com/edgelesssys/continuum/internal/oss/process"
)

// HTTPServer is simple HTTP health server.
//
// TODO: Remove once Contrast supports grpc probes.
type HTTPServer struct {
	log     *slog.Logger
	healthy *atomic.Bool
}

// NewHTTP initializes a new HTTPServer.
func NewHTTP(log *slog.Logger) *HTTPServer {
	return &HTTPServer{
		log:     log,
		healthy: &atomic.Bool{},
	}
}

// Serve starts the health server on the given endpoint.
func (s *HTTPServer) Serve(ctx context.Context, endpoint string) error {
	ls, err := net.Listen("tcp", endpoint)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	server := &http.Server{
		Addr:     endpoint,
		ErrorLog: slog.NewLogLogger(s.log.Handler(), slog.LevelError),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if s.healthy.Load() {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
			} else {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("NOT OK"))
			}
		}),
	}
	return process.HTTPServeContext(ctx, server, ls, s.log)
}

// SetHealthy marks the server as healthy.
func (s *HTTPServer) SetHealthy() {
	s.healthy.Store(true)
}

// SetUnhealthy marks the server as not healthy.
func (s *HTTPServer) SetUnhealthy() {
	s.healthy.Store(false)
}
