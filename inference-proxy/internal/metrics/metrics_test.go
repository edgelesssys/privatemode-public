package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer(t *testing.T) {
	testCases := map[string]struct {
		workloadMetricsURL string
		workloadReadyURL   string
		prober             prober
		serverReady        *atomic.Bool
		httpClient         httpClient
		metrics            []*dto.MetricFamily
		endpoint           string
		wantStatus         int
		wantBody           string
	}{
		"livez": {
			endpoint:   "/livez",
			wantStatus: http.StatusOK,
		},
		"readyz": {
			serverReady: func() *atomic.Bool { b := &atomic.Bool{}; b.Store(true); return b }(),
			endpoint:    "/readyz",
			wantStatus:  http.StatusOK,
		},
		"server not ready": {
			serverReady: &atomic.Bool{},
			endpoint:    "/readyz",
			wantStatus:  http.StatusServiceUnavailable,
		},
		"root endpoint": {
			endpoint:   "/",
			wantStatus: http.StatusNotFound,
		},
		"unknown endpoint": {
			endpoint:   "/foo",
			wantStatus: http.StatusNotFound,
		},
		"metrics local only": {
			workloadMetricsURL: "",
			metrics:            []*dto.MetricFamily{{Name: new("local_metric"), Metric: []*dto.Metric{{Counter: &dto.Counter{Value: new(2.)}}}}},
			endpoint:           "/metrics",
			wantStatus:         http.StatusOK,
			wantBody:           "# TYPE local_metric counter\nlocal_metric 2\n",
		},
		"metrics local invalid": {
			workloadMetricsURL: "",
			metrics:            []*dto.MetricFamily{{}},
			endpoint:           "/metrics",
			wantStatus:         http.StatusInternalServerError,
		},
		"metrics with workload": {
			workloadMetricsURL: "http://foo",
			httpClient:         fakeHTTPClient{status: http.StatusOK, body: "workload_metric 3\n"},
			metrics:            []*dto.MetricFamily{{Name: new("local_metric"), Metric: []*dto.Metric{{Counter: &dto.Counter{Value: new(2.)}}}}},
			endpoint:           "/metrics",
			wantStatus:         http.StatusOK,
			wantBody:           "# TYPE local_metric counter\nlocal_metric 2\nworkload_metric 3\n",
		},
		"metrics with workload error": {
			workloadMetricsURL: "http://foo",
			httpClient:         fakeHTTPClient{err: assert.AnError},
			endpoint:           "/metrics",
			wantStatus:         http.StatusInternalServerError,
		},
		"metrics with workload error status": {
			workloadMetricsURL: "http://workload.example/metrics",
			httpClient:         fakeHTTPClient{status: http.StatusUnauthorized},
			endpoint:           "/metrics",
			wantStatus:         http.StatusUnauthorized,
		},
		"workload livez not configured": {
			endpoint:   "/workload-livez",
			wantStatus: http.StatusNotFound,
		},
		"workload readyz not configured": {
			endpoint:   "/workload-readyz",
			wantStatus: http.StatusNotFound,
		},
		"workload livez healthy": {
			prober:     fakeProber{},
			endpoint:   "/workload-livez",
			wantStatus: http.StatusOK,
		},
		"workload livez unhealthy": {
			prober:     fakeProber{err: assert.AnError},
			endpoint:   "/workload-livez",
			wantStatus: http.StatusServiceUnavailable,
		},
		"workload readyz forwards status": {
			workloadReadyURL: "http://foo",
			httpClient:       fakeHTTPClient{status: http.StatusCreated},
			endpoint:         "/workload-readyz",
			wantStatus:       http.StatusCreated,
		},
		"workload readyz with request error": {
			workloadReadyURL: "http:/foo",
			httpClient:       fakeHTTPClient{err: assert.AnError},
			endpoint:         "/workload-readyz",
			wantStatus:       http.StatusInternalServerError,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			server := NewServer(tc.workloadMetricsURL, tc.workloadReadyURL, tc.prober, tc.serverReady, nil)
			server.httpClient = tc.httpClient
			server.metricsGatherer = stubGatherer{metrics: tc.metrics}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, tc.endpoint, http.NoBody)
			require.NoError(err)

			resp := httptest.NewRecorder()
			server.handler.ServeHTTP(resp, req)

			assert.Equal(tc.wantStatus, resp.Code)
			if tc.wantBody != "" {
				assert.Equal(tc.wantBody, resp.Body.String())
			}
		})
	}
}

type fakeProber struct{ err error }

func (p fakeProber) CheckLiveness(context.Context) error { return p.err }

type fakeHTTPClient struct {
	status int
	body   string
	err    error
}

func (c fakeHTTPClient) Do(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: c.status, Body: io.NopCloser(strings.NewReader(c.body))}, c.err
}

type stubGatherer struct {
	metrics []*dto.MetricFamily
}

func (g stubGatherer) Gather() ([]*dto.MetricFamily, error) {
	return g.metrics, nil
}
