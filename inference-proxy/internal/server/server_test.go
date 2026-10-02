package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter"
	"github.com/edgelesssys/continuum/inference-proxy/internal/secrets"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/encbody"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/ocsp"
	"github.com/edgelesssys/continuum/internal/usagecollector"
	"github.com/stretchr/testify/require"
)

// secretID is the inference secret ID used by the benchmark setup.
const secretID = "test"

func BenchmarkServeOpenAI(b *testing.B) {
	benchmarkServe(b, adapter.InferenceAPIOpenAI)
}

// benchmarkServe benchmarks how long it takes the server to serve requests.
// Tested in combination with https://hub.docker.com/r/ealen/echo-server.
func benchmarkServe(b *testing.B, apiType string) {
	require := require.New(b)

	log := slog.New(slog.DiscardHandler)

	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(w, r.Body)
	}))
	defer echoServer.Close()
	secret, payload, server := setup(b, apiType, echoServer.Listener.Addr().String(), log)

	proxyLis, err := net.Listen("tcp", "")
	require.NoError(err)

	// Build combined handler like the real server does.
	// Middleware is now applied per-route by each adapter.
	mux := http.NewServeMux()

	// Check if any adapter handles catch-all routing
	hasCatchAll := false
	for _, a := range server.adapters {
		if a.HandlesCatchAll() {
			hasCatchAll = true
			break
		}
	}
	if !hasCatchAll {
		mux.HandleFunc("/", adapter.UnsupportedEndpoint)
	}

	for _, a := range server.adapters {
		a.RegisterRoutes(mux)
	}

	go func() {
		if err := http.Serve(proxyLis, mux); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	// Needs a short sleep so server is up before client starts.
	time.Sleep(500 * time.Millisecond)
	endpoint := fmt.Sprintf("http://%s"+constants.OpenAIChatCompletionsEndpoint, proxyLis.Addr().String())

	b.Run("serve", func(b *testing.B) {
		// Reset timer so setup code is excluded from benchmark.
		b.ResetTimer()

		// Send b.N requests in parallel to learn how many concurrent requests the server can handle.
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				req, err := http.NewRequestWithContext(b.Context(), http.MethodPost, endpoint, bytes.NewBuffer(payload))
				require.NoError(err)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set(constants.PrivatemodeSecretIDHeader, secretID)
				exchange, err := encbody.InitializeClientExchange(secret)
				require.NoError(err)
				require.NoError(exchange.EncryptRequest(req))

				resp, err := http.DefaultClient.Do(req)
				require.NoError(err)

				if resp.StatusCode != http.StatusOK {
					respBody, err := io.ReadAll(resp.Body)
					require.NoError(err)
					b.Log("response:", string(respBody), "status:", resp.StatusCode)
					b.FailNow()
				}

				resp.Body.Close()
			}
		})

		b.StopTimer()

		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "requests/s")
	})
}

func setup(b *testing.B, apiType, workloadEndpoint string, log *slog.Logger) (secret, payload []byte, server *Server) {
	require := require.New(b)
	fw := forwarder.New(http.DefaultClient, workloadEndpoint, forwarder.SchemeHTTP, log)

	secret = []byte(strings.Repeat("a", 32))
	secrets := secrets.New(stubSecretGetter{})
	secrets.Reset(map[string][]byte{secretID: secret})

	payload = fmt.Appendf(
		nil,
		`{"model": "model", "messages": [{"role": "system","content": "You are a helpful assistant."},{"role": "user","content": "Hello!"}], "cache_salt": %q}`,
		strings.Repeat("a", 32),
	)

	ocspStatus, err := json.Marshal([]ocsp.StatusInfo{{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood}})
	require.NoError(err)
	ocspFile := filepath.Join(b.TempDir(), "ocsp.json")
	require.NoError(os.WriteFile(ocspFile, ocspStatus, 0o644))

	adapters, err := adapter.New([]string{apiType}, []string{"generate"}, secrets, ocspFile, fw, &stubUsageReporter{}, log)
	require.NoError(err)

	return secret, payload, New(adapters, nil, log)
}

type stubSecretGetter struct{}

func (s stubSecretGetter) GetSecret(_ context.Context, _ string) ([]byte, error) {
	return nil, secrets.ErrSecretNotFound
}

type stubUsageReporter struct{}

func (stubUsageReporter) Submit(_ context.Context, _ usagecollector.SubmitUsageReportRequest) error {
	return nil
}
