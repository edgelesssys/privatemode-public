// Package userapi package is responsible for handling requests
// from users to the attestation service.
package userapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/hpke"
	userpb "github.com/edgelesssys/continuum/internal/oss/proto/secret-service/userapi"
	"github.com/edgelesssys/continuum/internal/oss/secretexchange"
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
	meshCertRaw []byte
	meshPriv    *ecdsa.PrivateKey

	userpb.UnimplementedUserAPIServer
}

// New returns a new Server for the user API.
func New(tlsConfig *tls.Config, secretStore secretSetter, logger *slog.Logger) (*Server, error) {
	if tlsConfig == nil || len(tlsConfig.Certificates) != 1 {
		return nil, errors.New("expected a tlsConfig with exactly one certificate chain")
	}
	tlsCertChain := tlsConfig.Certificates[0]
	priv, ok := tlsCertChain.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("expected an ECDSA private key in the TLS certificate")
	}

	grpcServer := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: 15 * time.Second}),
	)

	s := &Server{
		grpc:                       grpcServer,
		secretStore:                secretStore,
		log:                        logger,
		meshCertRaw:                tlsCertChain.Certificate[0],
		meshPriv:                   priv,
		UnimplementedUserAPIServer: userpb.UnimplementedUserAPIServer{},
	}
	userpb.RegisterUserAPIServer(grpcServer, s)

	return s, nil
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

// ExchangeSecret performs a cryptographic key agreement.
func (s *Server) ExchangeSecret(ctx context.Context, req *userpb.ExchangeSecretRequest) (*userpb.ExchangeSecretResponse, error) {
	// Create HPKE encapsulated key and sender context.
	pub, err := hpke.MLKEM768X25519().NewPublicKey(req.PublicKey)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "parsing public key: %v", err)
	}
	encapKey, sender, err := hpke.NewSender(pub, hpke.HKDFSHA256(), hpke.ExportOnly(), nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "creating HPKE sender: %v", err)
	}

	// Sign the request (to prove freshness) and the response (to prove that it has been generated inside the TEE) with the mesh private key.
	signature, err := ecdsa.SignASN1(rand.Reader, s.meshPriv, secretexchange.Hash(req.PublicKey, encapKey))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "signing: %v", err)
	}

	// Store the shared secret.
	secret, err := sender.Export("", 32)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "exporting secret: %v", err)
	}
	secrets := map[string][]byte{secretexchange.ID(req.PublicKey): secret}
	if err := s.secretStore.SetSecrets(ctx, secrets, int64(time.Hour/time.Second)); err != nil {
		return nil, status.Errorf(codes.Internal, "saving secrets: %s", err)
	}

	return &userpb.ExchangeSecretResponse{
		EncapsulatedKey: encapKey,
		Signature:       signature,
		MeshCert:        s.meshCertRaw,
	}, nil
}

type secretSetter interface {
	SetSecrets(context.Context, map[string][]byte, int64) error
}
