// Package server implements the user facing server that receives requests and forwards them to the appropriate inference API adapter.
package server

import (
	"context"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter"
	"github.com/edgelesssys/continuum/internal/mtls"
	"github.com/edgelesssys/continuum/internal/oss/middleware"
	"github.com/edgelesssys/continuum/internal/oss/process"
	"github.com/edgelesssys/continuum/internal/oss/requestid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Server implements the user facing HTTP REST server.
type Server struct {
	adapters     []adapter.InferenceAdapter
	mtlsIdentity mtls.Identity
	log          *slog.Logger
}

// New creates a new Server.
func New(adapters []adapter.InferenceAdapter, mtlsIdentity mtls.Identity, log *slog.Logger) *Server {
	return &Server{
		adapters:     adapters,
		mtlsIdentity: mtlsIdentity,
		log:          log,
	}
}

// Serve starts the server.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	// Build combined ServeMux from all adapters.
	// Each adapter registers its routes with middleware already applied per-route.
	mux := http.NewServeMux()

	// Check if any adapter handles catch-all routing (e.g., unstructured, unencrypted).
	// If so, skip registering the server-level 501 handler to avoid conflicts.
	hasCatchAll := false
	for _, a := range s.adapters {
		if a.HandlesCatchAll() {
			hasCatchAll = true
			break
		}
	}
	if !hasCatchAll {
		mux.HandleFunc("/", adapter.UnsupportedEndpoint)
	}

	for _, a := range s.adapters {
		a.RegisterRoutes(mux)
	}

	// Apply middlewares below, handler holds the chain entrypoint
	var handler http.Handler = mux

	handler = tagRequestID(handler)

	// Span sampling is governed centrally by the tracer provider's sampler
	// (ParentBased + NeverSample root, see [tracing.Setup]):
	// The inference-proxy never originates traces, so parentless spans from
	// non-propagated internal traffic (e.g heartbeats) are dropped, while
	// spans continuing an inbound apigateway-sampled trace are recorded.
	handler = otelhttp.NewHandler(
		handler, "inference-proxy",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)

	handler = middleware.LogRequestSummary(handler, s.log, true)

	handler = s.addRequestID(handler)

	server := &http.Server{
		Addr:      listener.Addr().String(),
		Handler:   handler,
		TLSConfig: s.mtlsIdentity.ServerConfig(),
		ErrorLog:  newHTTPLogger(s.log), // Prometheus tries to scrape metrics from this TLS endpoint, causing errors we want to ignore
	}
	return process.HTTPServeContext(ctx, server, listener, s.log)
}

// addRequestID retrieves the request ID from the incoming request's header and stores it in the
// request context. If absent it logs a warning and generates a fresh request ID.
func (s *Server) addRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := requestid.FromHeader(r.Header)
		missing := requestID == requestid.Unknown
		if missing {
			requestID = requestid.New()
		}

		ctx := requestid.WithContext(r.Context(), requestID)
		if missing {
			s.log.WarnContext(
				ctx,
				"Generated fresh request ID because header is missing from incoming request",
			)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func tagRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestID, ok := requestid.FromContext(r.Context()); ok {
			trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("privatemode.request_id", requestID))
		}

		next.ServeHTTP(w, r)
	})
}

type httpLogger struct {
	infoLog  *log.Logger
	errorLog *log.Logger
}

func newHTTPLogger(slogger *slog.Logger) *log.Logger {
	return log.New(&httpLogger{
		infoLog:  slog.NewLogLogger(slogger.With("component", "httpErrorLog").Handler(), slog.LevelInfo),
		errorLog: slog.NewLogLogger(slogger.With("component", "httpErrorLog").Handler(), slog.LevelError),
	}, "", 0)
}

func (h *httpLogger) Write(b []byte) (n int, err error) {
	s := string(b)
	if strings.HasPrefix(s, "http: TLS handshake error from") {
		h.infoLog.Print(s)
	} else {
		h.errorLog.Print(s)
	}
	return len(b), nil
}
