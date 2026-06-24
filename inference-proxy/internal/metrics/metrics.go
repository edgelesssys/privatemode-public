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

	"github.com/edgelesssys/continuum/internal/oss/process"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

// Server is the metrics server.
type Server struct {
	workloadMetricsURL string
	log                *slog.Logger
	handler            http.Handler
	httpClient         httpClient
	metricsGatherer    prometheus.Gatherer
	metricsFormat      expfmt.Format
}

// NewServer creates a new Server.
func NewServer(workloadMetricsURL, workloadLiveURL, workloadReadyURL string, log *slog.Logger) *Server {
	mux := http.NewServeMux()
	s := &Server{
		workloadMetricsURL: workloadMetricsURL,
		log:                log,
		handler:            mux,
		httpClient:         http.DefaultClient,
		metricsGatherer:    prometheus.DefaultGatherer,
		metricsFormat:      expfmt.NewFormat(expfmt.TypeTextPlain),
	}
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/livez", handleHealth)
	mux.HandleFunc("/readyz", handleHealth)
	if workloadLiveURL != "" {
		mux.Handle("/workload-livez", s.workloadHealthHandler(workloadLiveURL))
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
		ErrorLog: slog.NewLogLogger(s.log.With("component", "metricsServer").Handler(), slog.LevelError),
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
		resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
	})
}

func handleHealth(_ http.ResponseWriter, _ *http.Request) {}

type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}
