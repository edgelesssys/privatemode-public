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
	"strings"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter"
	"github.com/edgelesssys/continuum/inference-proxy/internal/etcd"
	"github.com/edgelesssys/continuum/inference-proxy/internal/metrics"
	"github.com/edgelesssys/continuum/inference-proxy/internal/probe"
	"github.com/edgelesssys/continuum/inference-proxy/internal/server"
	"github.com/edgelesssys/continuum/inference-proxy/internal/statsfeed"
	"github.com/edgelesssys/continuum/inference-proxy/internal/usagereporter"
	"github.com/edgelesssys/continuum/internal/mtls"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/httputil"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/process"
	"github.com/edgelesssys/continuum/internal/tracing"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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

	cmd.Flags().StringVar(&cfg.listenPort, "listen-port", constants.InferenceProxyServerPort, "port the proxy server is listening on")
	cmd.Flags().StringVar(&cfg.metricsPort, "metrics-port", constants.MetricsServerPort, "port the metrics server is listening on")
	cmd.Flags().StringVar(&cfg.statsFeedPort, "stats-feed-port", constants.StatsFeedServerPort, "port the vLLM stats feed receiver is listening on")
	cmd.Flags().StringVar(&cfg.workloadPort, "workload-port", constants.WorkloadDefaultPort, "port the workload is listening on")
	cmd.Flags().StringSliceVar(&cfg.adapterTypes, "adapter-type", []string{"openai"}, "type of adapter to use (can be specified multiple times or comma-separated)")
	cmd.Flags().StringVar(&cfg.workloadAddress, "workload-address", "", "host name or IP the workload can be reached at over TCP")
	cmd.Flags().StringVar(&cfg.workloadMetricsURL, "workload-metrics", "", "URL of the workload metrics")
	cmd.Flags().StringVar(&cfg.workloadReadyURL, "workload-ready", "", "URL of the workload to check readiness")
	cmd.Flags().StringVar(&cfg.ssAddress, "secret-svc-address", "", "host name or IP for the secret service")
	cmd.Flags().StringVar(&cfg.etcdMemberCert, "etcd-member-cert", filepath.Join(constants.EtcdBasePath(), "etcd.crt"), "path to the etcd member certificate")
	cmd.Flags().StringVar(&cfg.etcdMemberKey, "etcd-member-key", filepath.Join(constants.EtcdBasePath(), "etcd.key"), "path to the etcd member key")
	cmd.Flags().StringVar(&cfg.etcdCA, "etcd-ca", filepath.Join(constants.EtcdBasePath(), "ca.crt"), "path to the etcd CA certificate")
	cmd.Flags().StringVar(&cfg.identityCertPath, "identity-cert-path", "", "path to the workload identity certificate")
	cmd.Flags().StringVar(&cfg.identityKeyPath, "identity-key-path", "", "path to the workload identity key")
	cmd.Flags().StringVar(&cfg.identityCAPath, "identity-ca-path", "", "path to the workload identity CA bundle (used to verify peer identity certs)")
	cmd.Flags().StringVar(&cfg.workloadTasks, "workload-tasks", "", "comma separated list of tasks the workload supports")
	cmd.Flags().StringVar(&cfg.workloadModel, "workload-model", "", "model name served by the workload, used for inference health probes")
	cmd.Flags().StringVar(&cfg.ocspStatusFile, "ocsp-status-file", constants.OCSPStatusFile(), "path to read the OCSP status file from")
	cmd.Flags().StringVar(&cfg.ucEndpoint, "usage-collector-endpoint", "", "host:port of the API gateway's usage collector RPC")
	cmd.Flags().StringVar(&cfg.logLevel, logging.Flag, logging.DefaultFlagValue, logging.FlagInfo)
	cmd.Flags().StringVar(&cfg.otlpEndpoint, tracing.EndpointFlag, "", tracing.EndpointFlagInfo)

	must(cmd.MarkFlagRequired("workload-address"))
	must(cmd.MarkFlagRequired("identity-cert-path"))
	must(cmd.MarkFlagRequired("identity-key-path"))
	must(cmd.MarkFlagRequired("identity-ca-path"))
	must(cmd.MarkFlagRequired("usage-collector-endpoint"))

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		log := logging.NewLogger(cfg.logLevel)
		log.InfoContext(cmd.Context(), "Continuum inference proxy", "version", constants.Version())

		return run(cmd.Context(), cfg, log)
	}

	return cmd
}

type runConfig struct {
	listenPort         string
	metricsPort        string
	statsFeedPort      string
	workloadPort       string
	adapterTypes       []string
	workloadAddress    string
	workloadMetricsURL string
	workloadReadyURL   string
	ssAddress          string
	etcdMemberCert     string
	etcdMemberKey      string
	etcdCA             string
	identityCertPath   string
	identityKeyPath    string
	identityCAPath     string
	workloadTasks      string
	workloadModel      string
	ocspStatusFile     string
	ucEndpoint         string
	logLevel           string
	otlpEndpoint       string
}

func run(ctx context.Context, cfg runConfig, log *slog.Logger) error {
	for _, adapterType := range cfg.adapterTypes {
		if !adapter.IsSupportedInferenceAPI(adapterType) {
			return fmt.Errorf("unsupported adapter type: %v", adapterType)
		}
	}
	log.InfoContext(ctx, "Starting inference proxy", "port", cfg.listenPort, "workloadPort", cfg.workloadPort, "adapterTypes", cfg.adapterTypes, "workloadAddress", cfg.workloadAddress)

	// The inference-proxy always sits behind the apigateway and never originates
	// traces itself: Never sample root spans. The ParentBased wrapper still
	// continues inbound, already-sampled traces forwarded by the apigateway.
	traceShutdown, err := tracing.Setup(ctx, "inference-proxy", cfg.otlpEndpoint, sdktrace.NeverSample(), log)
	if err != nil {
		return fmt.Errorf("setting up tracing: %w", err)
	}
	defer traceShutdown()

	ctx, cancel := process.SignalContext(ctx, os.Interrupt)
	defer cancel()

	tasks := strings.Split(cfg.workloadTasks, ",")

	log.InfoContext(ctx, "Setting up sync of inference secrets from etcd")
	fs := afero.Afero{Fs: afero.NewOsFs()}
	etcdWatcher, secrets, err := etcd.New([]string{cfg.ssAddress}, cfg.etcdMemberCert, cfg.etcdMemberKey, cfg.etcdCA, fs, log)
	if err != nil {
		return fmt.Errorf("creating etcd watcher: %w", err)
	}

	// Wrap the transport so outbound calls to vLLM get a client span
	// and the W3C traceparent header is injected automatically.
	workloadClient := &http.Client{Transport: otelhttp.NewTransport(httputil.NewTransport())}
	forwarder := forwarder.New(workloadClient, net.JoinHostPort(cfg.workloadAddress, cfg.workloadPort), forwarder.SchemeHTTP, log)

	mtlsIdentity, err := mtls.LoadIdentity(cfg.identityCertPath, cfg.identityKeyPath, cfg.identityCAPath)
	if err != nil {
		return fmt.Errorf("loading workload identity: %w", err)
	}

	usageReporter, err := usagereporter.New(cfg.ucEndpoint, mtlsIdentity)
	if err != nil {
		return fmt.Errorf("creating usage reporter: %w", err)
	}

	adapters, err := adapter.New(cfg.adapterTypes, tasks, secrets, cfg.ocspStatusFile, forwarder, usageReporter, log)
	if err != nil {
		return fmt.Errorf("creating adapters: %w", err)
	}
	server := server.New(adapters, mtlsIdentity, log)

	workloadBaseURL := "http://" + net.JoinHostPort(cfg.workloadAddress, cfg.workloadPort)

	wg, ctx := errgroup.WithContext(ctx)

	prober := probe.New(workloadBaseURL, cfg.workloadModel, tasks, log)

	metricsServer := metrics.NewServer(cfg.workloadMetricsURL, cfg.workloadReadyURL, prober, etcdWatcher.Ready, log)

	wg.Go(func() error {
		log.InfoContext(ctx, "Starting workload probe loop")
		prober.Run(ctx)
		return nil
	})

	wg.Go(func() error {
		log.InfoContext(ctx, "Starting metrics server", "port", cfg.metricsPort)
		listener, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", cfg.metricsPort))
		if err != nil {
			return fmt.Errorf("listening: %w", err)
		}
		if err := metricsServer.Serve(ctx, listener); err != nil {
			return fmt.Errorf("serving metrics server: %w", err)
		}
		return nil
	})

	wg.Go(func() error {
		log.InfoContext(ctx, "Starting stats feed server", "port", cfg.statsFeedPort)
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", cfg.statsFeedPort))
		if err != nil {
			return fmt.Errorf("listening: %w", err)
		}
		if err := statsfeed.New(log.With("component", "statsfeed")).Serve(ctx, listener); err != nil {
			return fmt.Errorf("serving stats feed server: %w", err)
		}
		return nil
	})

	wg.Go(func() error {
		log.InfoContext(ctx, "Starting server")
		listener, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", cfg.listenPort))
		if err != nil {
			return fmt.Errorf("listening: %w", err)
		}
		if err := server.Serve(ctx, listener); err != nil {
			return fmt.Errorf("serving: %w", err)
		}
		return nil
	})

	wg.Go(func() error {
		log.InfoContext(ctx, "Starting etcd watcher")
		if err := etcdWatcher.WatchSecrets(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("watching etcd secrets: %w", err)
		}
		return nil
	})

	return wg.Wait()
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
