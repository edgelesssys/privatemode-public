// Package tracing contains utility functions to set up OpenTelemetry distributed
// tracing for Privatemode components. Traces are exported via OTLP/gRPC.
package tracing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

const (
	// EndpointFlag is the flag name for the OTLP exporter endpoint.
	// Named to match the upstream vLLM flag for consistency.
	EndpointFlag = "otlp-traces-endpoint"
	// EndpointFlagInfo is the info string for the OTLP endpoint flag.
	EndpointFlagInfo = "OTLP/gRPC endpoint to export traces to (e.g. node-ip:4317). Empty disables tracing."
	// SampleRatioFlag is the flag name for the trace sampler probability.
	SampleRatioFlag = "trace-sample-ratio"
	// SampleRatioFlagInfo is the info string for the sample ratio flag.
	SampleRatioFlagInfo = "head-based trace sampler probability in [0.0, 1.0]"
	// DefaultSampleRatio is used when --trace-sample-ratio is not set.
	DefaultSampleRatio = 0.1
)

// shutdownTimeout bounds how long Shutdown waits to flush pending spans
// before giving up.
const shutdownTimeout = 5 * time.Second

// ShutdownFunc flushes pending spans and shuts down the tracer provider on
// a best-effort basis (bounded by an internal timeout; errors are logged).
// It should be called on process termination, typically via defer.
type ShutdownFunc func()

// Setup initializes the global OpenTelemetry tracer provider with an
// OTLP/gRPC exporter pointing at endpoint and sets the global propagator to W3C
// TraceContext + Baggage.
//
// rootSampler decides whether to sample spans that have no parent, i.e. traces
// this process originates itself. It is wrapped in a [sdktrace.ParentBased]
// sampler so that spans continuing an already-sampled inbound trace are always
// recorded regardless of rootSampler. Services that sit behind another traced
// service and should never start their own traces (e.g. inference-proxy)
// should pass [sdktrace.NeverSample].
//
// If endpoint is empty, tracing is disabled and a no-op shutdown function is
// returned. This allows services to be deployed without a collector.
func Setup(ctx context.Context, serviceName, endpoint string, rootSampler sdktrace.Sampler, log *slog.Logger) (ShutdownFunc, error) {
	// Always set the propagator so that incoming traceparent headers continue
	// existing traces even when this process does not export spans itself.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if endpoint == "" {
		log.InfoContext(ctx, "Tracing disabled (no OTLP endpoint configured)")
		return func() {}, nil
	}

	exporter, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
	}

	// Kubernetes identity (k8s.namespace.name, k8s.pod.name, ...) is added by the
	// Alloy collector's k8sattributes processor, which can also enrich traces
	// from senders we don't control (e.g. vLLM). We deliberately do not set those
	// attributes here: k8sattributes will not overwrite attributes that already
	// exist, so emitting them (even empty) would block enrichment.
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(constants.Version()),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("building tracer resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(rootSampler)),
	)
	otel.SetTracerProvider(tp)

	log.InfoContext(ctx, "Tracing enabled", "endpoint", endpoint, "service", serviceName)
	return func() { //nolint:contextcheck
		// Use a fresh context because the parent may already be cancelled by signal handling.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := tp.Shutdown(shutdownCtx); err != nil {
			log.WarnContext(shutdownCtx, "Shutting down tracer", "error", err)
		}
	}, nil
}
