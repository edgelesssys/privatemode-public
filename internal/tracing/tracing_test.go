package tracing

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestSetupDisabledWhenEndpointEmpty(t *testing.T) {
	log := slog.New(slog.DiscardHandler)

	shutdown, err := Setup(t.Context(), "svc", "", sdktrace.AlwaysSample(), log)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	shutdown()
}

func TestSetupConfiguresPropagatorAndTracer(t *testing.T) {
	log := slog.New(slog.DiscardHandler)

	// Setup constructs the OTLP exporter but does not eagerly dial - any
	// parseable target works for verifying configuration.
	shutdown, err := Setup(t.Context(), "svc", "127.0.0.1:0", sdktrace.AlwaysSample(), log)
	require.NoError(t, err)
	t.Cleanup(shutdown)

	prop := otel.GetTextMapPropagator()
	assert.Contains(t, prop.Fields(), "traceparent",
		"expected W3C TraceContext propagator")

	// Start a span, inject into outgoing headers, extract on the other side,
	// confirm trace ID is preserved.
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(t.Context(), "outbound")
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example/", nil)
	require.NoError(t, err)
	prop.Inject(req.Context(), propagation.HeaderCarrier(req.Header))
	require.NotEmpty(t, req.Header.Get("traceparent"))

	rr := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example/", nil)
	rr.Header.Set("traceparent", req.Header.Get("traceparent"))
	got := prop.Extract(t.Context(), propagation.HeaderCarrier(rr.Header))
	assert.Equal(
		t,
		span.SpanContext().TraceID(),
		trace.SpanContextFromContext(got).TraceID(),
	)
}
