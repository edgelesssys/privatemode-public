// Package metrics implements the metrics server that serves Prometheus metrics of the inference-proxy and the workload, as well as health probes.
package metrics

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"

	"github.com/edgelesssys/continuum/internal/oss/httputil"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/process"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

// Server is the metrics server.
type Server struct {
	workloadMetricsURL string
	prober             prober
	log                *slog.Logger
	handler            http.Handler
	httpClient         httpClient
	metricsGatherer    prometheus.Gatherer
	metricsFormat      expfmt.Format
	serverReady        *atomic.Bool
}

// NewServer creates a new Server.
func NewServer(workloadMetricsURL, workloadReadyURL string, prober prober, serverReady *atomic.Bool, log *slog.Logger) *Server {
	mux := http.NewServeMux()
	s := &Server{
		workloadMetricsURL: workloadMetricsURL,
		prober:             prober,
		log:                log,
		handler:            mux,
		httpClient:         httputil.NewClient(),
		metricsGatherer:    prometheus.DefaultGatherer,
		metricsFormat:      expfmt.NewFormat(expfmt.TypeTextPlain),
		serverReady:        serverReady,
	}
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/livez", handleHealth)
	mux.HandleFunc("/readyz", s.handleReadiness)
	if prober != nil {
		mux.HandleFunc("/workload-livez", s.workloadLivenessHandler)
	}
	if workloadReadyURL != "" {
		mux.Handle("/workload-readyz", s.workloadHealthHandler(workloadReadyURL))
	}
	return s
}

// Serve runs the server.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	server := &http.Server{
		Addr:     listener.Addr().String(),
		Handler:  s.handler,
		ErrorLog: logging.NewHTTPErrorLogger(s.log, "component", "metricsServer"),
	}
	return process.HTTPServeContext(ctx, server, listener, s.log)
}

// handleMetrics serves Prometheus metrics, combining local and workload metrics.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	localMetrics, err := s.metricsGatherer.Gather()
	if err != nil {
		http.Error(w, fmt.Sprintf("gathering local metrics: %v", err), http.StatusInternalServerError)
		return
	}
	encodedMetrics := &bytes.Buffer{}
	encoder := expfmt.NewEncoder(encodedMetrics, s.metricsFormat)
	for _, m := range localMetrics {
		if err := encoder.Encode(m); err != nil {
			http.Error(w, fmt.Sprintf("encoding local metrics: %v", err), http.StatusInternalServerError)
			return
		}
	}
	if s.workloadMetricsURL == "" {
		_, _ = io.Copy(w, encodedMetrics)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, s.workloadMetricsURL, http.NoBody)
	if err != nil {
		http.Error(w, fmt.Sprintf("creating workload metrics request: %v", err), http.StatusInternalServerError)
		return
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("getting workload metrics: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "getting workload metrics: "+resp.Status, resp.StatusCode)
		return
	}

	if _, err := io.Copy(w, encodedMetrics); err != nil {
		return
	}
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) workloadHealthHandler(url string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, http.NoBody)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp, err := s.httpClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
	})
}

func (s *Server) workloadLivenessHandler(w http.ResponseWriter, r *http.Request) {
	if err := s.prober.CheckLiveness(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
}

func (s *Server) handleReadiness(w http.ResponseWriter, _ *http.Request) {
	if !s.serverReady.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
}

func handleHealth(_ http.ResponseWriter, _ *http.Request) {}

type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}

type prober interface {
	CheckLiveness(ctx context.Context) error
}
