// Package traceforward forwards workload OTLP/gRPC exports to the trace collector.
package traceforward

import (
	"context"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/httputil"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/process"
)

const (
	// Match the collector's default 4 MiB gRPC message limit, plus the frame header.
	maxRequestBytes = (4 << 20) + 5
	exportTimeout   = 5 * time.Second
)

// Server forwards traces to the trace collector.
type Server struct {
	handler   http.Handler
	forwarder *forwarder.Forwarder
	transport *http.Transport
	log       *slog.Logger
}

// NewServer creates a forwarder to the fixed OTLP/gRPC collector at endpoint (host:port).
func NewServer(endpoint string, log *slog.Logger) *Server {
	transport := httputil.NewTransport()
	transport.Protocols = unencryptedHTTP2Protocols()
	// Export directly to the configured collector, independent of HTTP proxy settings.
	transport.Proxy = nil

	client := &http.Client{
		Transport:     transport,
		Timeout:       exportTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}

	// Do not wrap this client in tracing middleware: exporting traces must not
	// generate more traces, or change the identity of vLLM's original spans.
	mux := http.NewServeMux()
	s := &Server{
		handler:   mux,
		forwarder: forwarder.New(client, endpoint, forwarder.SchemeHTTP, log),
		transport: transport,
		log:       log,
	}
	mux.HandleFunc("POST /", s.handleExport)
	return s
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (contentType != "application/grpc" && !strings.HasPrefix(contentType, "application/grpc+")) {
		http.Error(w, "expected OTLP/gRPC", http.StatusUnsupportedMediaType)
		return
	}

	var upstream *http.Response
	s.forwarder.Forward(w, r, func(req *http.Request) error {
		// The forwarder strips hop-by-hop headers. gRPC requires this one.
		req.Header.Set("Te", "trailers")
		return nil
	}, func(resp *http.Response) (forwarder.Response, error) {
		upstream = resp
		return forwarder.PassthroughResponseMapper(resp)
	}, forwarder.WithMaxBodyBytes(maxRequestBytes, "trace export too large"))

	// Trailers are populated only after the upstream body has been consumed.
	// Preserve grpc-status, grpc-message, and grpc-status-details-bin so the
	// workload's exporter receives the collector's success/failure and retries.
	if upstream != nil {
		for key, values := range upstream.Trailer {
			w.Header()[http.TrailerPrefix+key] = values
		}
	}
}

// Serve runs the OTLP/gRPC receiver. The caller must supply a loopback listener.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	defer s.transport.CloseIdleConnections()
	server := &http.Server{
		Handler:           s.handler,
		Protocols:         unencryptedHTTP2Protocols(),
		ReadHeaderTimeout: exportTimeout,
		ReadTimeout:       2 * exportTimeout,
		ErrorLog:          logging.NewHTTPErrorLogger(s.log, "component", "traceforward"),
	}
	defer server.Close()
	return process.HTTPServeContext(ctx, server, listener, s.log)
}

// unencryptedHTTP2Protocols sets up [*http.Protocols] for forwarding unencrypted gRPC connections.
func unencryptedHTTP2Protocols() *http.Protocols {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	return protocols
}
