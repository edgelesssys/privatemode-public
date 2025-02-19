// package backendapi implements the backend API for the attestation service.
// It handles interaction with workloads, and is not publicly exposed to users.
package backendapi

import (
	context "context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/edgelesssys/continuum/internal/etcd/builder"
	backendpb "github.com/edgelesssys/continuum/internal/proto/secret-service/backendapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

// Server handles communication with workers.
type Server struct {
	grpc     *grpc.Server
	pki      etcdPKI
	logger   *slog.Logger
	hostname string

	backendpb.UnimplementedBackendAPIServer
}

// New returns a new Server to obtain access to secrets.
func New(hostname string, tlsConfig *tls.Config, pki etcdPKI, logger *slog.Logger) *Server {
	grpcServer := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: 15 * time.Second}),
	)

	s := &Server{
		grpc:                          grpcServer,
		pki:                           pki,
		hostname:                      hostname,
		logger:                        logger,
		UnimplementedBackendAPIServer: backendpb.UnimplementedBackendAPIServer{},
	}
	backendpb.RegisterBackendAPIServer(grpcServer, s)

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

// AccessSecrets provides a verified attestation-agent with secrets.
func (s *Server) AccessSecrets(_ context.Context, _ *backendpb.AccessSecretsRequest) (res *backendpb.AccessSecretsResponse, err error) {
	s.logger.Info("AccessSecrets called")
	defer func() {
		if err != nil {
			s.logger.Error("AccessSecrets failed", "error", err)
		} else {
			s.logger.Info("AccessSecrets succeeded")
		}
	}()

	// Worker nodes are not meant to be long lived infrastructure
	// e.g. they will be replaced by node upgrades, and can simply shutdown or replaced
	// We might want to consider an even shorter validity period
	sans := []string{s.hostname}
	validity := time.Hour * 24 * 365
	clientCert, clientKey, err := s.pki.CreateCertificate(builder.EtcdClientName, sans, nil, validity)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "creating client certificate: %s", err)
	}
	caCert := s.pki.CACertificate()

	return &backendpb.AccessSecretsResponse{
		Cert:   clientCert,
		Key:    clientKey,
		CACert: caCert,
	}, nil
}

type etcdPKI interface {
	CreateCertificate(commonName string, sans []string, ips []net.IP, validity time.Duration) ([]byte, []byte, error)
	CACertificate() []byte
}
