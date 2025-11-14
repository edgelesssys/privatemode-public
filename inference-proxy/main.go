// main package of Continuum's inference proxy.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter"
	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/inference-proxy/internal/etcd"
	"github.com/edgelesssys/continuum/inference-proxy/internal/secrets"
	"github.com/edgelesssys/continuum/inference-proxy/internal/server"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/process"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/afero"
	"golang.org/x/sync/errgroup"
)

var (
	listenPort      = flag.String("listen-port", constants.ProxyServerPort, "port the proxy server is listening on")
	metricsPort     = flag.String("metrics-port", constants.MetricsServerPort, "port the metrics server is listening on")
	workloadPort    = flag.String("workload-port", constants.WorkloadDefaultExposedPort, "port the workload is listening on")
	adapterType     = flag.String("adapter-type", "openai", "type of adapter to use")
	workloadAddress = flag.String("workload-address", "", "host name or IP the workload can be reached at over TCP")
	ssAddress       = flag.String("secret-svc-address", "", "host name or IP for the secret service.")
	etcdMemberCert  = flag.String("etcd-member-cert", filepath.Join(constants.EtcdBasePath(), "etcd.crt"), "path to the etcd member certificate")
	etcdMemberKey   = flag.String("etcd-member-key", filepath.Join(constants.EtcdBasePath(), "etcd.key"), "path to the etcd member key")
	etcdCA          = flag.String("etcd-ca", filepath.Join(constants.EtcdBasePath(), "ca.crt"), "path to the etcd CA certificate")
	workloadTask    = flag.String("workload-tasks", "", "comma separated list of tasks the workload supports")
	ocspStatusFile  = flag.String("ocsp-status-file", constants.OCSPStatusFile(), "path to read the OCSP status file from")
	logLevel        = flag.String(logging.Flag, logging.DefaultFlagValue, logging.FlagInfo)
)

func main() {
	flag.Parse()
	log := logging.NewLogger(*logLevel)
	log.Info("Continuum inference proxy", "version", constants.Version())

	if err := run(log); err != nil {
		log.Error("Error running inference-proxy", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	if *workloadAddress == "" {
		return errors.New("flag --workload-address must be provided")
	}

	// Preliminary check if the adapter type is supported
	if !adapter.IsSupportedInferenceAPI(*adapterType) {
		return fmt.Errorf("unsupported adapter type: %v", *adapterType)
	}
	log.Info("Starting inference proxy", "port", constants.ProxyServerPort, "workloadPort", *workloadPort, "adapterType", *adapterType, "workloadAddress", *workloadAddress)

	ctx, cancel := process.SignalContext(context.Background(), os.Interrupt)
	defer cancel()

	tasks := strings.Split(*workloadTask, ",")

	secrets := secrets.New(stubSecretGetter{}, nil)
	if *adapterType != adapter.InferenceAPIUnencrypted {
		var closeClient func()
		var err error
		secrets, closeClient, err = setUpEtcdSync(ctx, *ssAddress, *etcdMemberCert, *etcdMemberKey, *etcdCA, log)
		if err != nil {
			return fmt.Errorf("setting up etcd sync: %w", err)
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

	forwarder := forwarder.New(&http.Client{}, net.JoinHostPort(*workloadAddress, *workloadPort), forwarder.SchemeHTTP, log)

	adapter, err := adapter.New(*adapterType, tasks, cipher.New(secrets), *ocspStatusFile, forwarder, log)
	if err != nil {
		return fmt.Errorf("creating adapter: %w", err)
	}
	server := server.New(adapter, log)

	wg, ctx := errgroup.WithContext(ctx)

	wg.Go(func() error {
		log.Info("Starting metrics server", "port", *metricsPort)
		mux := http.NewServeMux()
		mux.Handle(constants.MetricsEndpoint, promhttp.Handler())

		listener, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", *metricsPort))
		if err != nil {
			return fmt.Errorf("listening: %w", err)
		}
		metricsServer := &http.Server{
			Addr:     listener.Addr().String(),
			Handler:  mux,
			ErrorLog: slog.NewLogLogger(log.With("component", "metricsServer").Handler(), slog.LevelError),
		}

		if err := process.HTTPServeContext(ctx, metricsServer, listener, log); err != nil {
			return fmt.Errorf("serving metrics server: %w", err)
		}
		return nil
	})

	wg.Go(func() error {
		log.Info("Starting server")
		listener, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", *listenPort))
		if err != nil {
			return fmt.Errorf("listening: %w", err)
		}
		if err := server.Serve(ctx, listener); err != nil {
			return fmt.Errorf("serving: %w", err)
		}
		return nil
	})

	return wg.Wait()
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

type stubSecretGetter struct{}

func (s stubSecretGetter) GetSecret(_ context.Context, _ string) ([]byte, error) {
	return nil, errors.New("not found")
}
