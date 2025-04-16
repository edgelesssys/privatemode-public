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
	"path/filepath"
	"sync"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/contrast"
	"github.com/edgelesssys/continuum/internal/gpl/logging"
	"github.com/edgelesssys/continuum/secret-service/internal/etcd"
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
	etcdHost := flag.String("etcd-host", "secret-service-internal.continuum.svc.cluster.local", "host name to set up the initial etcd cluster")
	etcdServerCert := flag.String("etcd-server-cert", filepath.Join(constants.EtcdBasePath(), "etcd.crt"), "path to the etcd server certificate")
	etcdServerKey := flag.String("etcd-server-key", filepath.Join(constants.EtcdBasePath(), "etcd.key"), "path to the etcd server key")
	etcdCA := flag.String("etcd-ca", filepath.Join(constants.EtcdBasePath(), "ca.crt"), "path to the etcd CA certificate")
	logLevel := flag.String(logging.Flag, logging.DefaultFlagValue, logging.FlagInfo)
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log := logging.NewLogger(*logLevel)
	log.Info("Continuum Secret Service", "version", constants.Version())

	if err := run(ctx, *etcdHost, *port, *healthPort, *etcdServerCert, *etcdServerKey, *etcdCA, afero.Afero{Fs: afero.NewOsFs()}, log); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

func run(
	ctx context.Context, etcdHost, serverPort, healthPort string,
	etcdServerCert, etcdServerKey, etcdCA string,
	fs afero.Afero, log *slog.Logger,
) error {
	// Set up etcd
	etcd, etcdClose, err := etcd.New(ctx, etcdHost, etcdServerCert, etcdServerKey, etcdCA, fs, log)
	if err != nil {
		return err
	}
	defer etcdClose()

	contrastMTLS, err := contrast.ServerTLSConfig("")
	if err != nil {
		return fmt.Errorf("setting up Contrast TLS config: %w", err)
	}
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
		}
	}()

	wg.Wait()
	return err
}
