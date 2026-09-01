// main package of the secret-service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/edgelesssys/continuum/internal/mtls"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/contrast"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/process"
	"github.com/edgelesssys/continuum/secret-service/internal/etcd"
	"github.com/edgelesssys/continuum/secret-service/internal/health"
	"github.com/edgelesssys/continuum/secret-service/internal/userapi"
	"github.com/spf13/afero"
	"golang.org/x/sync/errgroup"
)

const (
	defaultHost = "0.0.0.0"

	// noLeaderThreshold is the duration after which the health server is marked as unhealthy
	// if the etcd cluster has no leader. This causes the Kubernetes liveness probe to fail,
	// triggering a container restart so the cluster can re-bootstrap and recover quorum.
	noLeaderThreshold = 2 * time.Minute
	// noLeaderCheckInterval is how often we check the etcd leader status.
	noLeaderCheckInterval = 10 * time.Second
)

func main() {
	port := flag.String("port", constants.SecretServiceUserPort, "port to listen on")
	healthPort := flag.String("health-port", constants.SecretServiceHealthPort, "port for health probes")
	etcdServerCert := flag.String("etcd-server-cert", filepath.Join(constants.EtcdBasePath(), "etcd.crt"), "path to the etcd server certificate")
	etcdServerKey := flag.String("etcd-server-key", filepath.Join(constants.EtcdBasePath(), "etcd.key"), "path to the etcd server key")
	etcdCA := flag.String("etcd-ca", filepath.Join(constants.EtcdBasePath(), "ca.crt"), "path to the etcd CA certificate")
	identityCertPath := flag.String("identity-cert-path", "", "path to the workload identity certificate")
	identityKeyPath := flag.String("identity-key-path", "", "path to the workload identity key")
	identityCAPath := flag.String("identity-ca-path", "", "path to the workload identity CA bundle (used to verify peer identity certs)")
	k8sNamespace := flag.String("k8s-namespace", "", "kubernetes namespace of this secret-service instance")
	logLevel := flag.String(logging.Flag, logging.DefaultFlagValue, logging.FlagInfo)
	mayBootstrap := flag.Bool("may-bootstrap", false, "whether this instance is allowed to bootstrap the etcd cluster")
	flag.Parse()

	log := logging.NewLogger(*logLevel)
	log.InfoContext(context.Background(), "Continuum Secret Service", "version", constants.Version())

	config := secretServiceConfig{
		port:             *port,
		healthPort:       *healthPort,
		etcdServerCert:   *etcdServerCert,
		etcdServerKey:    *etcdServerKey,
		etcdCA:           *etcdCA,
		identityCertPath: *identityCertPath,
		identityKeyPath:  *identityKeyPath,
		identityCAPath:   *identityCAPath,
		k8sNamespace:     *k8sNamespace,
		mayBootstrap:     *mayBootstrap,
	}

	if err := run(config, afero.Afero{Fs: afero.NewOsFs()}, log); err != nil {
		log.ErrorContext(context.Background(), err.Error())
		os.Exit(1)
	}
}

type secretServiceConfig struct {
	port             string
	healthPort       string
	etcdServerCert   string
	etcdServerKey    string
	etcdCA           string
	identityCertPath string
	identityKeyPath  string
	identityCAPath   string
	k8sNamespace     string
	mayBootstrap     bool
}

func run(config secretServiceConfig, fs afero.Afero, log *slog.Logger) error {
	ctx, cancel := process.SignalContext(context.Background(), os.Interrupt)
	defer cancel()

	etcdServer, etcdClose, err := joinOrBootstrapEtcd(ctx, config, fs, log)
	if err != nil {
		return fmt.Errorf("joining or bootstrapping etcd: %w", err)
	}
	defer etcdClose()

	meshTLSConfig, err := contrast.ServerTLSConfig("")
	if err != nil {
		return fmt.Errorf("setting up Contrast TLS config: %w", err)
	}
	mtlsIdentity, err := mtls.LoadIdentity(config.identityCertPath, config.identityKeyPath, config.identityCAPath)
	if err != nil {
		return fmt.Errorf("loading workload identity: %w", err)
	}
	userServer, err := userapi.New(meshTLSConfig, mtlsIdentity, etcdServer, log)
	if err != nil {
		return fmt.Errorf("setting up user server: %w", err)
	}
	healthServer := health.New(log)

	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		watchLeaderStatus(groupCtx, etcdServer, healthServer, log)
		return nil
	})

	group.Go(func() error {
		endpoint := net.JoinHostPort(defaultHost, config.port)
		log.InfoContext(groupCtx, "Starting user server", "endpoint", endpoint)
		if err := userServer.Serve(groupCtx, endpoint); err != nil && groupCtx.Err() == nil {
			return fmt.Errorf("serving user API: %w", err)
		}
		return nil
	})

	group.Go(func() error {
		endpoint := net.JoinHostPort(defaultHost, config.healthPort)
		log.InfoContext(groupCtx, "Starting health server", "endpoint", endpoint)
		if err := healthServer.Serve(groupCtx, endpoint); err != nil && groupCtx.Err() == nil {
			return fmt.Errorf("serving health API: %w", err)
		}
		return nil
	})

	return group.Wait()
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
	log.InfoContext(ctx, "Discovering existing etcd cluster")
	joinCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	etcdServer, etcdClose, err := etcd.New(joinCtx, etcd.Join, config.k8sNamespace,
		config.etcdServerCert, config.etcdServerKey, config.etcdCA, fs, log)
	if etcdServer != nil {
		// If an existing cluster is found, return the etcd server and a no-op close function
		log.InfoContext(ctx, "Found existing etcd cluster, joining it")
		return etcdServer, etcdClose, nil
	}
	var joinErr *etcd.JoinError
	if !errors.As(err, &joinErr) {
		return nil, nil, fmt.Errorf("unexpected error while discovering etcd cluster: %w", err)
	}
	log.InfoContext(ctx, "Etcd discovery failed, proceeding to bootstrap or wait for bootstrapper instance",
		"error", err)

	if config.mayBootstrap {
		// Step 2: If no existing cluster is found, and this instance is the etcd bootstrapper instance, bootstrap a new cluster
		log.InfoContext(ctx, "No existing etcd cluster found, bootstrapping a new cluster")
		etcdServer, etcdClose, err := etcd.New(ctx, etcd.Bootstrap, config.k8sNamespace,
			config.etcdServerCert, config.etcdServerKey, config.etcdCA, fs, log)
		if err != nil {
			return nil, nil, fmt.Errorf("bootstrapping etcd: %w", err)
		}
		return etcdServer, etcdClose, nil
	}

	// Step 3: If no existing cluster is found and this instance is not the etcd bootstrapper instance, wait for the bootstrapper instance
	log.InfoContext(ctx, "No existing etcd cluster found, waiting for the bootstrapper instance to bootstrap a new cluster")
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			return nil, nil, fmt.Errorf("timed out waiting for etcd bootstrapper instance to bootstrap a cluster: %w", waitCtx.Err())
		case <-ticker.C:
			log.InfoContext(ctx, "Checking if cluster has been bootstrapped yet")
			joinCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			etcdServer, etcdClose, err := etcd.New(joinCtx, etcd.Join, config.k8sNamespace,
				config.etcdServerCert, config.etcdServerKey, config.etcdCA, fs, log)
			if etcdServer != nil {
				log.InfoContext(ctx, "Successfully joined etcd cluster")
				return etcdServer, etcdClose, nil
			}
			log.InfoContext(ctx, "Cluster not yet bootstrapped, retrying", "error", err)
		}
	}
}

// watchLeaderStatus periodically checks whether the etcd cluster has a leader.
// If the cluster has been leaderless for longer than [noLeaderThreshold], the health server
// is marked as unhealthy, causing the Kubernetes liveness probe to fail and the pod to be restarted.
func watchLeaderStatus(ctx context.Context, e *etcd.Etcd, h *health.Server, log *slog.Logger) {
	ticker := time.NewTicker(noLeaderCheckInterval)
	defer ticker.Stop()

	var leaderlessSince time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if e.HasLeader() {
				if !leaderlessSince.IsZero() {
					log.InfoContext(ctx, "Etcd cluster has regained a leader")
					leaderlessSince = time.Time{}
				}
				h.SetHealthy(true)
				continue
			}

			if leaderlessSince.IsZero() {
				leaderlessSince = time.Now()
			}
			leaderlessFor := time.Since(leaderlessSince)
			log.WarnContext(ctx, "Etcd cluster has no leader", "leaderlessFor", leaderlessFor.Seconds(), "threshold", noLeaderThreshold.Seconds())

			if leaderlessFor >= noLeaderThreshold {
				log.ErrorContext(ctx, "Etcd cluster has been leaderless for too long, marking pod as unhealthy for restart",
					"leaderlessFor", leaderlessFor.Seconds(), "threshold", noLeaderThreshold.Seconds())
				h.SetHealthy(false)
			}
		}
	}
}
