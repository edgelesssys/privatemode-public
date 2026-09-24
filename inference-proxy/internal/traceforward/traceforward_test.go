package traceforward

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func TestForwardGRPC(t *testing.T) {
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	upstream := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("test-service", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(upstream, healthServer)
	t.Cleanup(upstream.Stop)
	go func() { assert.NoError(t, upstream.Serve(upstreamListener)) }()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	go func() { finished <- NewServer(upstreamListener.Addr().String(), slog.Default()).Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-finished:
			if !errors.Is(err, context.Canceled) {
				assert.NoError(t, err)
			}
		case <-time.After(5 * time.Second):
			t.Error("trace forwarder did not stop")
		}
	})

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })
	client := healthpb.NewHealthClient(conn)
	callCtx, timeoutCancel := context.WithTimeout(ctx, 5*time.Second)
	defer timeoutCancel()

	response, err := client.Check(callCtx, &healthpb.HealthCheckRequest{Service: "test-service"})
	require.NoError(t, err)
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, response.GetStatus())

	_, err = client.Check(callCtx, &healthpb.HealthCheckRequest{Service: "unknown-service"})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestRejectInvalidRequests(t *testing.T) {
	for name, tc := range map[string]struct {
		method, path, contentType string
		body                      string
		status                    int
	}{
		"wrong method":    {http.MethodGet, "/export", "application/grpc", "", http.StatusMethodNotAllowed},
		"wrong protocol":  {http.MethodPost, "/export", "application/json", "{}", http.StatusUnsupportedMediaType},
		"oversized batch": {http.MethodPost, "/export", "application/grpc", strings.Repeat("x", maxRequestBytes+1), http.StatusRequestEntityTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("rejected request reached collector") }))
			defer upstream.Close()
			server := NewServer(upstream.Listener.Addr().String(), slog.Default())
			defer server.transport.CloseIdleConnections()
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			response := httptest.NewRecorder()
			server.handler.ServeHTTP(response, req)
			assert.Equal(t, tc.status, response.Code)
		})
	}
}
