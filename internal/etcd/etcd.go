// package etcd manages Continuum's etcd key-value store.
// The etcd server is started as a subroutine of the Attestation Service.
// Each AS instance runs its own etcd server.
// For distributed deployments, this means that each node runs its own etcd server,
// which are joined into a cluster over the network.
package etcd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/edgelesssys/continuum/internal/crypto"
	"github.com/edgelesssys/continuum/internal/etcd/builder"
	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/spf13/afero"
	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// Etcd is a handle for Continuum's etcd key-value store backend.
// The etcd server is directly started as a routine of the binary importing this package.
type Etcd struct {
	server etcdInf

	etcdMemberCert *x509.Certificate
	log            *slog.Logger
}

// New sets up etcd on the node and returns a client to securely interact with it.
// The returned close function gracefully shuts down the etcd server.
func New(ctx context.Context, host string, ca pki, fs afero.Afero, log *slog.Logger) (*Etcd, func(), error) {
	if err := fs.MkdirAll(constants.EtcdBasePath(), 0o600); err != nil {
		return nil, nil, fmt.Errorf("creating etcd base directory: %w", err)
	}

	memberCertPEM, err := createEtcdMemberKeyPair(host, ca, fs, log)
	if err != nil {
		return nil, nil, fmt.Errorf("generating etcd member certificate: %w", err)
	}
	memberCert, err := crypto.ParseCertificateFromPEM(memberCertPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing etcd member certificate: %w", err)
	}

	server, err := builder.StartNewCluster(authCtx(ctx, memberCert), host)
	if err != nil {
		return nil, nil, fmt.Errorf("setting up etcd: %w", err)
	}
	// Wait for etcd server to start (and join an existing etcd cluster if necessary)
	select {
	case <-server.Server.ReadyNotify():
	case <-time.After(60 * time.Second):
		// TODO: Check if we want to do this at all
		// This was taken from the etcd embed documentation
		// We might not want to error out here, but rather log a warning
		// and retry joining the cluster
		server.Close()
		return nil, nil, errors.New("etcd took too long to start")
	}

	e := &Etcd{
		etcdMemberCert: memberCert,
		log:            log,
		server:         &etcdServer{server},
	}
	return e, e.server.Close, nil
}

// SetSecrets saves the given secrets in the etcd backend.
// The operation will either succeed for all, or fail for all.
// If any of the new secrets already exist, the operation will fail.
func (e *Etcd) SetSecrets(ctx context.Context, secrets map[string][]byte, ttl int64) (retErr error) {
	var errs []error
	var ifs []*pb.Compare
	var thens []*pb.RequestOp
	var elses []*pb.RequestOp

	var leaseID int64
	if ttl > 0 {
		// Create a lease for the secrets
		leaseResp, err := e.server.LeaseGrant(authCtx(ctx, e.etcdMemberCert), &pb.LeaseGrantRequest{
			TTL: ttl,
			ID:  0, // Let etcd generate a lease ID for us
		})
		if err != nil {
			return fmt.Errorf("creating lease for secrets: %w", err)
		}
		leaseID = leaseResp.ID

		defer func() {
			if retErr != nil {
				if _, err = e.server.LeaseRevoke(authCtx(ctx, e.etcdMemberCert), &pb.LeaseRevokeRequest{ID: leaseResp.ID}); err != nil {
					e.log.Warn("Failed to revoke lease after failed transaction", "error", err, "leaseID", leaseResp.ID)
				}
			}
		}()
	}

	for id, secret := range secrets {
		keyID := constants.EtcdInferenceSecretPrefix + id

		// IF the key does not exist (CreateRevision == 0)
		cmp := clientv3.Compare(clientv3.CreateRevision(keyID), "=", 0)
		ifs = append(ifs, (*pb.Compare)(&cmp))

		// THEN put the secret
		thens = append(thens, &pb.RequestOp{Request: &pb.RequestOp_RequestPut{RequestPut: &pb.PutRequest{
			Key:   []byte(keyID),
			Value: secret,
			Lease: leaseID,
		}}})

		// ELSE get the secret, so we can write an error message
		// This is a limitation of the transaction, because we otherwise don't know which key failed
		elses = append(elses, &pb.RequestOp{Request: &pb.RequestOp_RequestRange{RequestRange: &pb.RangeRequest{Key: []byte(keyID)}}})
	}

	// Execute the transaction
	resp, err := e.server.Txn(authCtx(ctx, e.etcdMemberCert), &pb.TxnRequest{
		Compare: ifs,
		Success: thens,
		Failure: elses,
	})
	if err != nil {
		return fmt.Errorf("writing transaction to etcd: %w", err)
	}

	// Failing to commit a transaction does not return an error
	// Instead, we manually check the response for success
	// and write a helpful error message if it failed
	if !resp.Succeeded {
		for _, r := range resp.Responses {
			get := r.GetResponseRange()

			// filter for just the keys that already exist
			if get == nil || len(get.Kvs) == 0 {
				continue
			}

			keyID := strings.TrimPrefix(string(get.Kvs[0].Key), constants.EtcdInferenceSecretPrefix)

			errs = append(errs, fmt.Errorf("secret %q already exists", keyID))
		}
		// Fallback in case the get operations failed,
		// or another issue occurred with the transaction
		if errs == nil {
			return errors.New("failed writing secrets to etcd")
		}
	}

	return errors.Join(errs...)
}

// DeleteSecrets deletes the list of secrets from the etcd backend.
// The operation will either succeed for all, or fail for all.
// If any of the secret that should be deleted don't exist, the operation will fail.
func (e *Etcd) DeleteSecrets(ctx context.Context, secrets []string) error {
	var ifs []*pb.Compare
	var thens []*pb.RequestOp

	for _, id := range secrets {
		keyID := constants.EtcdInferenceSecretPrefix + id
		// IF the key exists (CreateRevision > 0)
		cmp := clientv3.Compare(clientv3.CreateRevision(keyID), ">", 0)
		ifs = append(ifs, (*pb.Compare)(&cmp))

		// THEN delete the secret
		thens = append(thens, &pb.RequestOp{Request: &pb.RequestOp_RequestDeleteRange{RequestDeleteRange: &pb.DeleteRangeRequest{
			Key: []byte(keyID),
		}}})

		// ELSE do nothing
	}

	// Execute the transaction
	resp, err := e.server.Txn(authCtx(ctx, e.etcdMemberCert), &pb.TxnRequest{
		Compare: ifs,
		Success: thens,
	})
	if err != nil {
		return fmt.Errorf("writing transaction to etcd: %w", err)
	}

	if !resp.Succeeded {
		return errors.New("failed deleting secrets from etcd. Does the secret exist?")
	}

	return nil
}

// authCtx wraps the given context with grpc metadata and peer information containing the etcd member certificate.
// This is required because etcd's gRPC methods themselves perform authentication based on the client certificate
// parsed from the context.
func authCtx(ctx context.Context, etcdMemberCert *x509.Certificate) context.Context {
	return metadata.NewIncomingContext(peer.NewContext(ctx, &peer.Peer{
		AuthInfo: credentials.TLSInfo{
			State: tls.ConnectionState{
				PeerCertificates: []*x509.Certificate{
					etcdMemberCert,
				},
				VerifiedChains: [][]*x509.Certificate{
					{etcdMemberCert},
				},
			},
		},
		Addr:      nil,
		LocalAddr: nil,
	}), nil)
}

func createEtcdMemberKeyPair(host string, ca pki, fs afero.Afero, log *slog.Logger) ([]byte, error) {
	var keyExists, certExists bool
	if _, err := fs.Stat(filepath.Join(constants.EtcdPKIPath(), "etcd.crt")); err == nil {
		certExists = true
	}
	if _, err := fs.Stat(filepath.Join(constants.EtcdPKIPath(), "etcd.key")); err == nil {
		keyExists = true
	}
	if certExists && keyExists {
		log.Info("etcd member key pair already exists, skipping generation")

		return fs.ReadFile(filepath.Join(constants.EtcdPKIPath(), "etcd.crt"))
	}

	if certExists || keyExists {
		log.Warn("Incomplete etcd member key pair found. Creating new key pair.", "certificateExists", certExists, "keyExists", keyExists)
	} else {
		log.Info("Creating new etcd member key pair")
	}

	// Generate certificate for the etcd server
	etcdSANs := []string{
		"localhost",
	}
	// Inspired by the default SANs given to etcd certs by kubeadm: https://kubernetes.io/docs/setup/best-practices/certificates/#all-certificates
	etcdIPs := []net.IP{
		net.IPv4zero,
		net.ParseIP("127.0.0.1"),
		net.IPv6zero,
		net.IPv6loopback,
	}

	if hostIP := net.ParseIP(host); hostIP != nil {
		etcdIPs = append(etcdIPs, hostIP)
	} else {
		etcdSANs = append(etcdSANs, host)
	}

	etcdCommonName := "root"
	// etcd certificates are valid for one year. This is the same default as used by Kubernetes
	// TODO: certificates should be renewed on Continuum node image upgrades
	// TODO: consider implementing an API endpoint to renew certificates
	validity := time.Hour * 24 * 365

	etcdCert, etcdKey, err := ca.CreateCertificate(etcdCommonName, etcdSANs, etcdIPs, validity)
	if err != nil {
		return nil, err
	}

	// Save the certificate and key to disk
	if err := fs.MkdirAll(constants.EtcdPKIPath(), 0o600); err != nil {
		return nil, fmt.Errorf("creating etcd PKI directory: %w", err)
	}
	if err := fs.WriteFile(filepath.Join(constants.EtcdPKIPath(), "etcd.crt"), etcdCert, 0o600); err != nil {
		return nil, fmt.Errorf("writing etcd member certificate: %w", err)
	}
	if err := fs.WriteFile(filepath.Join(constants.EtcdPKIPath(), "etcd.key"), etcdKey, 0o600); err != nil {
		return nil, fmt.Errorf("writing etcd member key: %w", err)
	}

	return etcdCert, nil
}

type etcdServer struct {
	*embed.Etcd
}

func (s *etcdServer) Txn(ctx context.Context, req *pb.TxnRequest) (*pb.TxnResponse, error) {
	return s.Server.Txn(ctx, req)
}

func (s *etcdServer) LeaseGrant(ctx context.Context, req *pb.LeaseGrantRequest) (*pb.LeaseGrantResponse, error) {
	return s.Server.LeaseGrant(ctx, req)
}

func (s *etcdServer) LeaseRevoke(ctx context.Context, req *pb.LeaseRevokeRequest) (*pb.LeaseRevokeResponse, error) {
	return s.Server.LeaseRevoke(ctx, req)
}

func (s *etcdServer) Close() {
	s.Etcd.Close()
}

type etcdInf interface {
	Txn(context.Context, *pb.TxnRequest) (*pb.TxnResponse, error)
	LeaseGrant(context.Context, *pb.LeaseGrantRequest) (*pb.LeaseGrantResponse, error)
	LeaseRevoke(context.Context, *pb.LeaseRevokeRequest) (*pb.LeaseRevokeResponse, error)
	Close()
}

type pki interface {
	CreateCertificate(commonName string, sans []string, ips []net.IP, validity time.Duration) (certPEM []byte, keyPEM []byte, err error)
}
