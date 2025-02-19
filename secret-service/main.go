// main package of the secret-service.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/edgelesssys/continuum/internal/etcd"
	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/contrast"
	"github.com/edgelesssys/continuum/internal/gpl/logging"
	"github.com/edgelesssys/continuum/internal/pki"
	"github.com/edgelesssys/continuum/secret-service/internal/backendapi"
	"github.com/edgelesssys/continuum/secret-service/internal/health"
	"github.com/edgelesssys/continuum/secret-service/internal/userapi"
	"github.com/spf13/afero"
)

const (
	defaultHost = "0.0.0.0"
)

func main() {
	port := flag.String("port", constants.SecretServiceUserPort, "port to listen on")
	healthPort := flag.String("health-port", constants.AttestationServiceHealthPort, "port for health probes")
	etcdCertSANs := flag.String("etcd-cert-sans", "secret-service-internal.continuum.svc.cluster.local", "subjective alternative names to use for the secret service's etcd TLS certificate")
	logLevel := flag.String(logging.Flag, logging.DefaultFlagValue, logging.FlagInfo)
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log := logging.NewLogger(*logLevel)
	log.Info("Continuum Secret Service", "version", constants.Version())

	if err := run(ctx, *etcdCertSANs, *port, *healthPort, afero.Afero{Fs: afero.NewOsFs()}, log); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

func run(
	ctx context.Context, etcdCertSANs, serverPort, healthPort string,
	fs afero.Afero, log *slog.Logger,
) error {
	// Set up a new PKI
	pki, err := pki.New(nil, nil, fs, log)
	if err != nil {
		return err
	}

	// Set up etcd
	etcd, etcdClose, err := etcd.New(ctx, etcdCertSANs, pki, fs, log)
	if err != nil {
		return err
	}
	defer etcdClose()

	contrastMTLS, err := contrast.ServerTLSConfig("")
	if err != nil {
		return fmt.Errorf("setting up Contrast TLS config: %w", err)
	}
	backendServer := backendapi.New(etcdCertSANs, contrastMTLS, pki, log)
	contrastTLS := contrastMTLS.Clone()
	contrastTLS.ClientAuth = tls.NoClientCert // the user API should not enforce mTLS
	userServer := userapi.New(contrastTLS, etcd, log)
	healthServer := health.New(log)

	var wg sync.WaitGroup

	// Start the servers as Goroutines
	// If one of them fails, the routine will stop the other server and return the error

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("Starting user server", "endpoint", net.JoinHostPort(defaultHost, serverPort))
		if srvErr := userServer.Serve(net.JoinHostPort(defaultHost, serverPort)); srvErr != nil {
			err = srvErr
			backendServer.Stop()
			healthServer.Stop()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("Starting backend server", "endpoint", net.JoinHostPort(defaultHost, constants.AttestationServiceBackendPort))
		if srvErr := backendServer.Serve(net.JoinHostPort(defaultHost, constants.AttestationServiceBackendPort)); srvErr != nil {
			err = srvErr
			userServer.Stop()
			healthServer.Stop()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("Starting health server", "endpoint", net.JoinHostPort(defaultHost, healthPort))
		if srvErr := healthServer.Serve(net.JoinHostPort(defaultHost, healthPort)); srvErr != nil {
			err = srvErr
			userServer.Stop()
			backendServer.Stop()
		}
	}()

	wg.Wait()
	return err
}
