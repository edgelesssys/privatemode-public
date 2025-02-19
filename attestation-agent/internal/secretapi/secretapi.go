// Package secretapi provides a client to access the Secret Service API.
package secretapi

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"

	"github.com/avast/retry-go/v4"
	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/grpc/dialer"
	"github.com/edgelesssys/continuum/internal/proto/secret-service/backendapi"
	"github.com/spf13/afero"
)

// RequestSecretAccess requests secret access from the Secret Service.
func RequestSecretAccess(ctx context.Context, log *slog.Logger, ssAddress string, tlsConfig *tls.Config) (*backendapi.AccessSecretsResponse, error) {
	dialer := dialer.New(&net.Dialer{}, tlsConfig)
	secretEndpoint := net.JoinHostPort(ssAddress, constants.SecretServiceBackendPort)
	conn, err := dialer.NewConn(secretEndpoint)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var secretAccess *backendapi.AccessSecretsResponse

	err = retry.Do(
		func() error {
			backendClient := backendapi.NewBackendAPIClient(conn)
			secretAccess, err = backendClient.AccessSecrets(ctx, &backendapi.AccessSecretsRequest{})
			if err != nil {
				return err
			}
			return nil
		},
		retry.OnRetry(func(n uint, err error) {
			log.Warn("Retrying secret access request with Secret Service", "attempt", n+1, "error", err)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire secret access after retries: %w", err)
	}

	log.Info("Successfully acquired secret access from Secret Service")

	return secretAccess, nil
}

// SaveSecretAccessCerts saves the secret access certificates to the filesystem.
func SaveSecretAccessCerts(fs afero.Afero, resp *backendapi.AccessSecretsResponse) error {
	if err := fs.MkdirAll(constants.EtcdPKIPath(), 0o700); err != nil {
		return fmt.Errorf("creating etcd PKI directory: %w", err)
	}

	files := map[string][]byte{
		"ca.crt":     resp.CACert,
		"client.crt": resp.Cert,
		"client.key": resp.Key,
	}

	for filename, data := range files {
		path := filepath.Join(constants.EtcdPKIPath(), filename)
		if err := fs.WriteFile(path, data, 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", filename, err)
		}
	}

	return nil
}
