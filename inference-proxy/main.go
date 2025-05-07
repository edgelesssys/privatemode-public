// main package of Continuum's inference proxy.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter"
	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/inference-proxy/internal/etcd"
	"github.com/edgelesssys/continuum/inference-proxy/internal/secrets"
	"github.com/edgelesssys/continuum/inference-proxy/internal/server"
	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/internal/gpl/logging"
	"github.com/spf13/afero"
)

var (
	workloadPort    = flag.String("workload-port", constants.WorkloadDefaultExposedPort, "port the workload is listening on")
	adapterType     = flag.String("adapter-type", "openai", "type of adapter to use")
	workloadAddress = flag.String("workload-address", "", "host name or IP the workload can be reached at over TCP")
	ssAddress       = flag.String("secret-svc-address", "", "host name or IP for the secret service.")
	etcdMemberCert  = flag.String("etcd-member-cert", filepath.Join(constants.EtcdBasePath(), "etcd.crt"), "path to the etcd member certificate")
	etcdMemberKey   = flag.String("etcd-member-key", filepath.Join(constants.EtcdBasePath(), "etcd.key"), "path to the etcd member key")
	etcdCA          = flag.String("etcd-ca", filepath.Join(constants.EtcdBasePath(), "ca.crt"), "path to the etcd CA certificate")
	workloadTask    = flag.String("workload-task", "", "task the workload is running")
	logLevel        = flag.String(logging.Flag, logging.DefaultFlagValue, logging.FlagInfo)
)

func main() {
	flag.Parse()
	log := logging.NewLogger(*logLevel)
	log.Info("Continuum inference proxy", "version", constants.Version())

	if *workloadAddress == "" {
		log.Error("flag --workload-address must be provided")
		os.Exit(1)
	}

	// Preliminary check if the adapter type is supported
	if !adapter.IsSupportedInferenceAPI(*adapterType) {
		log.Error("Unsupported adapter type", "adapterType", *adapterType)
		os.Exit(1)
	}
	log.Info("Starting inference proxy", "port", constants.ProxyServerPort, "workloadPort", *workloadPort, "adapterType", *adapterType, "workloadAddress", *workloadAddress)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	secrets := secrets.New(nil)
	if *adapterType != adapter.InferenceAPIUnencrypted {
		var closeClient func()
		var err error
		secrets, closeClient, err = setUpEtcdSync(ctx, *ssAddress, *etcdMemberCert, *etcdMemberKey, *etcdCA, log)
		if err != nil {
			log.Error("Failed to set up etcd sync", "error", err)
			os.Exit(1)
		}
		defer closeClient()
	} else {
		fmt.Println("-----------------------------------------------------")
		fmt.Println("-----------------------WARNING-----------------------")
		fmt.Println("Using API adapter without encryption")
		fmt.Println("This is insecure and should only be used for testing")
		fmt.Println("-----------------------WARNING-----------------------")
		fmt.Println("-----------------------------------------------------")
		log.Warn("Skipping etcd set up since the inference proxy is running an unencrypted API adapter")
	}

	forwarder := forwarder.New("tcp", net.JoinHostPort(*workloadAddress, *workloadPort), log)

	adapter, err := adapter.New(*adapterType, *workloadTask, cipher.New(secrets), forwarder, log)
	if err != nil {
		log.Error("Failed to create adapter", "error", err)
		os.Exit(1)
	}
	server := server.New(adapter, log)

	log.Info("Starting server")
	listener, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", constants.ProxyServerPort))
	if err != nil {
		log.Error("Failed to listen", "error", err)
		os.Exit(1)
	}
	if err := server.Serve(listener); err != nil {
		log.Error("Failed to start server", "error", err)
		os.Exit(1)
	}
}

func setUpEtcdSync(ctx context.Context, address, etcdMemberCert, etcdMemberKey, etcdCA string, log *slog.Logger) (*secrets.Secrets, func(), error) {
	log.Info("Setting up sync of inference secrets from etcd")
	fs := afero.Afero{Fs: afero.NewOsFs()}

	etcdWatcher, closeClient, err := etcd.New([]string{address}, etcdMemberCert, etcdMemberKey, etcdCA, fs, log)
	if err != nil {
		return nil, nil, fmt.Errorf("creating etcd watcher: %w", err)
	}

	log.Info("Starting sync of inference secrets")
	secrets, err := etcdWatcher.WatchSecrets(ctx)
	if err != nil {
		closeClient()
		return nil, nil, fmt.Errorf("starting secrets watcher: %w", err)
	}

	return secrets, closeClient, nil
}
