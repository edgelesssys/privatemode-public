// main package of the secret-service.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/contrast"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/process"
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
	etcdServerCert := flag.String("etcd-server-cert", filepath.Join(constants.EtcdBasePath(), "etcd.crt"), "path to the etcd server certificate")
	etcdServerKey := flag.String("etcd-server-key", filepath.Join(constants.EtcdBasePath(), "etcd.key"), "path to the etcd server key")
	etcdCA := flag.String("etcd-ca", filepath.Join(constants.EtcdBasePath(), "ca.crt"), "path to the etcd CA certificate")
	k8sNamespace := flag.String("k8s-namespace", "", "kubernetes namespace of this secret-service instance")
	logLevel := flag.String(logging.Flag, logging.DefaultFlagValue, logging.FlagInfo)
	mayBootstrap := flag.Bool("may-bootstrap", false, "whether this instance is allowed to bootstrap the etcd cluster")
	flag.Parse()

	log := logging.NewLogger(*logLevel)
	log.Info("Continuum Secret Service", "version", constants.Version())

	config := secretServiceConfig{
		port:           *port,
		healthPort:     *healthPort,
		etcdServerCert: *etcdServerCert,
		etcdServerKey:  *etcdServerKey,
		etcdCA:         *etcdCA,
		k8sNamespace:   *k8sNamespace,
		mayBootstrap:   *mayBootstrap,
	}

	if err := run(config, afero.Afero{Fs: afero.NewOsFs()}, log); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

type secretServiceConfig struct {
	port           string
	healthPort     string
	etcdServerCert string
	etcdServerKey  string
	etcdCA         string
	k8sNamespace   string
	mayBootstrap   bool
}

func run(config secretServiceConfig, fs afero.Afero, log *slog.Logger) error {
	ctx, cancel := process.SignalContext(context.Background(), os.Interrupt)
	defer cancel()

	etcdServer, etcdClose, err := joinOrBootstrapEtcd(ctx, config, fs, log)
	if err != nil {
		return fmt.Errorf("joining or bootstrapping etcd: %w", err)
	}
	defer etcdClose()

	contrastMTLS, err := contrast.ServerTLSConfig("")
	if err != nil {
		return fmt.Errorf("setting up Contrast TLS config: %w", err)
	}
	contrastTLS := contrastMTLS.Clone()
	contrastTLS.ClientAuth = tls.NoClientCert // the user API should not enforce mTLS
	userServer := userapi.New(contrastTLS, etcdServer, log)
	healthServer := health.New(log)

	var wg sync.WaitGroup

	// Start the servers as Goroutines
	// If one of them fails, the routine will stop the other server and return the error

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("Starting user server", "endpoint", net.JoinHostPort(defaultHost, config.port))
		if srvErr := userServer.Serve(net.JoinHostPort(defaultHost, config.port)); srvErr != nil {
			err = srvErr
			healthServer.Stop()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("Starting health server", "endpoint", net.JoinHostPort(defaultHost, config.healthPort))
		if srvErr := healthServer.Serve(net.JoinHostPort(defaultHost, config.healthPort)); srvErr != nil {
			err = srvErr
			userServer.Stop()
		}
	}()

	wg.Wait()
	return err
}

// joinOrBootstrapEtcd sets up the etcd cluster by either joining an existing cluster or bootstrapping a new one.
// It does so by performing the following steps:
//
//  1. Try to discover an existing etcd cluster in the network. If one exists, it will join the cluster.
//  2. If no existing cluster is found, and if the current instance is marked as the etcd bootstrapper instance,
//     it will bootstrap a new etcd cluster.
//  3. If no existing cluster is found and the current instance is not the etcd bootstrapper instance,
//     it will wait for the bootstrapper instance to bootstrap the cluster.
//
// The returned close function is expected to be handled by the caller to gracefully shut down the etcd server.
func joinOrBootstrapEtcd(
	ctx context.Context, config secretServiceConfig, fs afero.Afero, log *slog.Logger,
) (*etcd.Etcd, func(), error) {
	// Step 1: Try to discover an existing etcd cluster
	log.Info("Discovering existing etcd cluster")
	joinCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	etcdServer, etcdClose, err := etcd.New(joinCtx, etcd.Join, config.k8sNamespace,
		config.etcdServerCert, config.etcdServerKey, config.etcdCA, fs, log)
	if etcdServer != nil {
		// If an existing cluster is found, return the etcd server and a no-op close function
		log.Info("Found existing etcd cluster, joining it")
		return etcdServer, etcdClose, nil
	}
	var joinErr *etcd.JoinError
	if !errors.As(err, &joinErr) {
		return nil, nil, fmt.Errorf("unexpected error while discovering etcd cluster: %w", err)
	}
	log.Info("Etcd discovery failed, proceeding to bootstrap or wait for bootstrapper instance",
		"error", err)

	if config.mayBootstrap {
		// Step 2: If no existing cluster is found, and this instance is the etcd bootstrapper instance, bootstrap a new cluster
		log.Info("No existing etcd cluster found, bootstrapping a new cluster")
		etcdServer, etcdClose, err := etcd.New(ctx, etcd.Bootstrap, config.k8sNamespace,
			config.etcdServerCert, config.etcdServerKey, config.etcdCA, fs, log)
		if err != nil {
			return nil, nil, fmt.Errorf("bootstrapping etcd: %w", err)
		}
		return etcdServer, etcdClose, nil
	}

	// Step 3: If no existing cluster is found and this instance is not the etcd bootstrapper instance, wait for the bootstrapper instance
	log.Info("No existing etcd cluster found, waiting for the bootstrapper instance to bootstrap a new cluster")
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			return nil, nil, fmt.Errorf("timed out waiting for etcd bootstrapper instance to bootstrap a cluster: %w", waitCtx.Err())
		case <-ticker.C:
			log.Info("Checking if cluster has been bootstrapped yet")
			joinCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			etcdServer, etcdClose, err := etcd.New(joinCtx, etcd.Join, config.k8sNamespace,
				config.etcdServerCert, config.etcdServerKey, config.etcdCA, fs, log)
			if etcdServer != nil {
				log.Info("Successfully joined etcd cluster")
				return etcdServer, etcdClose, nil
			}
			log.Info("Cluster not yet bootstrapped, retrying", "error", err)
		}
	}
}
