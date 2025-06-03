package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter"
	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/inference-proxy/internal/secrets"
	crypto "github.com/edgelesssys/continuum/internal/gpl/crypto"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/stretchr/testify/require"
)

func BenchmarkServeUnencrypted(b *testing.B) {
	benchmarkServe(b, adapter.InferenceAPIUnencrypted)
}

func BenchmarkServeOpenAI(b *testing.B) {
	benchmarkServe(b, adapter.InferenceAPIOpenAI)
}

// benchmarkServe benchmarks how long it takes the server to serve requests.
// Tested in combination with https://hub.docker.com/r/ealen/echo-server.
func benchmarkServe(b *testing.B, apiType string) {
	require := require.New(b)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(w, r.Body)
	}))
	defer echoServer.Close()
	payload, server := setup(b, apiType, echoServer.Listener.Addr().String(), log)

	proxyLis, err := net.Listen("tcp", "")
	require.NoError(err)
	go func() {
		if err := server.serveInsecure(proxyLis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	// Needs a short sleep so server is up before client starts.
	time.Sleep(500 * time.Millisecond)
	endpoint := fmt.Sprintf("http://%s/v1/chat/completions", proxyLis.Addr().String())

	b.Run("serve", func(b *testing.B) {
		// Reset timer so setup code is excluded from benchmark.
		b.ResetTimer()

		// Send b.N requests in parallel to learn how many concurrent requests the server can handle.
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				req, err := http.NewRequestWithContext(b.Context(), http.MethodPost, endpoint, bytes.NewBuffer(payload))
				require.NoError(err)

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

func setup(b *testing.B, apiType, workloadEndpoint string, log *slog.Logger) ([]byte, *Server) {
	require := require.New(b)
	fw := forwarder.New("tcp", workloadEndpoint, log)

	secret := []byte("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	c := cipher.New(secrets.New(stubSecretGetter{}, map[string][]byte{"test": secret}))
	plain := `[{"role": "system","content": "You are a helpful assistant."},{"role": "user","content": "Hello!"}]`
	requestCipher, err := crypto.NewRequestCipher(secret, "test")
	require.NoError(err)
	m, err := requestCipher.Encrypt(plain)
	require.NoError(err)
	payload := fmt.Sprintf(`{"model": "model","messages": %s}`, m)

	adapter, err := adapter.New(apiType, []string{"generate"}, c, fw, log)
	require.NoError(err)

	server := New(adapter, log)

	return []byte(payload), server
}

type stubSecretGetter struct{}

func (s stubSecretGetter) GetSecret(_ context.Context, _ string) ([]byte, error) {
	return nil, errors.New("not found")
}
