// Package health implements a gRPC health check server
package health

import (
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// Server handles health check requests.
type Server struct {
	grpcHealth *grpc.Server
	health     *health.Server
	logger     *slog.Logger
}

// New initializes a new Server.
func New(logger *slog.Logger) *Server {
	s := &Server{
		grpcHealth: grpc.NewServer(),
		health:     health.NewServer(),
		logger:     logger,
	}

	grpc_health_v1.RegisterHealthServer(s.grpcHealth, s.health)
	return s
}

// Serve starts the health server on the given endpoint.
func (s *Server) Serve(endpoint string) error {
	lis, err := net.Listen("tcp", endpoint)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.health.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	return s.grpcHealth.Serve(lis)
}

// Stop stops the server.
func (s *Server) Stop() {
	s.grpcHealth.GracefulStop()
}
