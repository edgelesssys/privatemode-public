// main package of Continuum's inference proxy.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inference-proxy",
		Short: "Continuum inference proxy",
		Long:  "Proxy server that handles encryption/decryption of inference API requests.",
	}

	var cfg runConfig

	cmd.Flags().StringVar(&cfg.listenPort, "listen-port", constants.ProxyServerPort, "port the proxy server is listening on")
	cmd.Flags().StringVar(&cfg.metricsPort, "metrics-port", constants.MetricsServerPort, "port the metrics server is listening on")
	cmd.Flags().StringVar(&cfg.workloadPort, "workload-port", constants.WorkloadDefaultExposedPort, "port the workload is listening on")
	cmd.Flags().StringSliceVar(&cfg.adapterTypes, "adapter-type", []string{"openai"}, "type of adapter to use (can be specified multiple times or comma-separated)")
	cmd.Flags().StringVar(&cfg.workloadAddress, "workload-address", "", "host name or IP the workload can be reached at over TCP")
	cmd.Flags().StringVar(&cfg.ssAddress, "secret-svc-address", "", "host name or IP for the secret service")
	cmd.Flags().StringVar(&cfg.etcdMemberCert, "etcd-member-cert", filepath.Join(constants.EtcdBasePath(), "etcd.crt"), "path to the etcd member certificate")
	cmd.Flags().StringVar(&cfg.etcdMemberKey, "etcd-member-key", filepath.Join(constants.EtcdBasePath(), "etcd.key"), "path to the etcd member key")
	cmd.Flags().StringVar(&cfg.etcdCA, "etcd-ca", filepath.Join(constants.EtcdBasePath(), "ca.crt"), "path to the etcd CA certificate")
	cmd.Flags().StringVar(&cfg.workloadTasks, "workload-tasks", "", "comma separated list of tasks the workload supports")
	cmd.Flags().StringVar(&cfg.ocspStatusFile, "ocsp-status-file", constants.OCSPStatusFile(), "path to read the OCSP status file from")
	cmd.Flags().StringVar(&cfg.logLevel, logging.Flag, logging.DefaultFlagValue, logging.FlagInfo)

	_ = cmd.MarkFlagRequired("workload-address")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		log := logging.NewLogger(cfg.logLevel)
		log.Info("Continuum inference proxy", "version", constants.Version())

		return run(cmd.Context(), cfg, log)
	}

	return cmd
}

type runConfig struct {
	listenPort      string
	metricsPort     string
	workloadPort    string
	adapterTypes    []string
	workloadAddress string
	ssAddress       string
	etcdMemberCert  string
	etcdMemberKey   string
	etcdCA          string
	workloadTasks   string
	ocspStatusFile  string
	logLevel        string
}

func run(ctx context.Context, cfg runConfig, log *slog.Logger) error {
	for _, adapterType := range cfg.adapterTypes {
		if !adapter.IsSupportedInferenceAPI(adapterType) {
			return fmt.Errorf("unsupported adapter type: %v", adapterType)
		}
	}
	log.Info("Starting inference proxy", "port", cfg.listenPort, "workloadPort", cfg.workloadPort, "adapterTypes", cfg.adapterTypes, "workloadAddress", cfg.workloadAddress)

	ctx, cancel := process.SignalContext(ctx, os.Interrupt)
	defer cancel()

	tasks := strings.Split(cfg.workloadTasks, ",")

	// Only if no encryption adapter is rqeuested, etcd can be omitted.
	needsEtcd := slices.ContainsFunc(cfg.adapterTypes, func(adapterType string) bool {
		return adapterType != adapter.InferenceAPIUnencrypted
	})

	secrets := secrets.New(stubSecretGetter{}, nil)
	if needsEtcd {
		var closeClient func()
		var err error
		secrets, closeClient, err = setUpEtcdSync(ctx, cfg.ssAddress, cfg.etcdMemberCert, cfg.etcdMemberKey, cfg.etcdCA, log)
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

	forwarder := forwarder.New(&http.Client{}, net.JoinHostPort(cfg.workloadAddress, cfg.workloadPort), forwarder.SchemeHTTP, log)

	adapters, err := adapter.New(cfg.adapterTypes, tasks, cipher.New(secrets), cfg.ocspStatusFile, forwarder, log)
	if err != nil {
		return fmt.Errorf("creating adapters: %w", err)
	}
	server := server.New(adapters, log)

	wg, ctx := errgroup.WithContext(ctx)

	wg.Go(func() error {
		log.Info("Starting metrics server", "port", cfg.metricsPort)
		mux := http.NewServeMux()
		mux.Handle(constants.MetricsEndpoint, promhttp.Handler())

		listener, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", cfg.metricsPort))
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
		listener, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", cfg.listenPort))
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
