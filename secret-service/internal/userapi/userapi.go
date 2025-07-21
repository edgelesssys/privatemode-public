// Package userapi package is responsible for handling requests
// from users to the attestation service.
package userapi

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	userpb "github.com/edgelesssys/continuum/internal/gpl/proto/secret-service/userapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

// Server handles communication with users.
type Server struct {
	grpc        *grpc.Server
	secretStore secretSetter
	log         *slog.Logger

	userpb.UnimplementedUserAPIServer
}

// New returns a new Server for the user API.
func New(tlsConfig *tls.Config, secretStore secretSetter, logger *slog.Logger) *Server {
	grpcServer := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: 15 * time.Second}),
	)

	s := &Server{
		grpc:                       grpcServer,
		secretStore:                secretStore,
		log:                        logger,
		UnimplementedUserAPIServer: userpb.UnimplementedUserAPIServer{},
	}
	userpb.RegisterUserAPIServer(grpcServer, s)

	return s
}

// Serve starts the server on the given endpoint.
func (s *Server) Serve(endpoint string) error {
	lis, err := net.Listen("tcp", endpoint)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	return s.grpc.Serve(lis)
}

// Stop stops the server.
func (s *Server) Stop() {
	s.grpc.GracefulStop()
}

// SetSecrets sets user secrets for the attestation service.
// The attestation service is tasked with distributing secrets to workers.
func (s *Server) SetSecrets(ctx context.Context, req *userpb.SetSecretsRequest) (*userpb.SetSecretsResponse, error) {
	s.log.Info("SetSecrets called")

	// Sanity check for correct secret length
	var errs []error
	for id, secret := range req.Secrets {
		switch len(secret) {
		case 16, 24, 32: // AES-128, AES-192, AES-256
		default:
			errs = append(errs, fmt.Errorf("secret %q has invalid length: %d", id, len(secret)))
		}
	}
	if len(errs) > 0 {
		return nil, status.Errorf(
			codes.InvalidArgument,
			"invalid secret format: secrets must be 16 (AES-128), 24 (AES-192), or 32 (AES-256) bytes long: %s",
			errors.Join(errs...),
		)
	}

	// Store the secrets.
	if err := s.secretStore.SetSecrets(ctx, req.Secrets, req.TimeToLive); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to save secrets: %s", err)
	}

	return &userpb.SetSecretsResponse{}, nil
}

type secretSetter interface {
	SetSecrets(context.Context, map[string][]byte, int64) error
}
