package anthropic

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/inference"
	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/oss/anthropic"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/ocsp"
	"github.com/edgelesssys/continuum/internal/oss/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	defaultModel  = "claude-3-opus-20240229"
	testCacheSalt = "dGVzdHNhbHR0ZXN0c2FsdHRlc3RzYWx0dGVzdHNhbHQ=" // 32 bytes base64-encoded
)

func TestForwardMessagesRequest(t *testing.T) {
	testCases := map[string]struct {
		clientRequest    string
		serverResponse   string
		validateResponse func(assert *assert.Assertions, responseRecorder *httptest.ResponseRecorder)
	}{
		"success": {
			clientRequest: func() string {
				res, err := json.Marshal(anthropic.MessagesRequest{
					MessagesRequestPlainData: anthropic.MessagesRequestPlainData{
						Model: defaultModel,
					},
					MaxTokens: 1024,
					Messages: []anthropic.Message{
						{Role: "user", Content: "Hello, Claude!"},
					},
					CacheSalt: testCacheSalt,
				})
				require.NoError(t, err)
				return string(res)
			}(),
			serverResponse: func() string {
				res, err := json.Marshal(anthropic.MessagesResponse{
					ID:         "msg_123",
					Type:       "message",
					Role:       "assistant",
					Content:    []anthropic.ContentBlock{{Type: "text", Text: "Hello!"}},
					Model:      defaultModel,
					StopReason: "end_turn",
					Usage:      anthropic.Usage{InputTokens: 10, OutputTokens: 5},
				})
				require.NoError(t, err)
				return string(res)
			}(),
			validateResponse: func(assert *assert.Assertions, responseRecorder *httptest.ResponseRecorder) {
				assert.Equal(http.StatusOK, responseRecorder.Code)
			},
		},
		"with streaming": {
			clientRequest: func() string {
				res, err := json.Marshal(anthropic.MessagesRequest{
					MessagesRequestPlainData: anthropic.MessagesRequestPlainData{
						Model:  defaultModel,
						Stream: true,
					},
					MaxTokens: 1024,
					Messages: []anthropic.Message{
						{Role: "user", Content: "Hello, Claude!"},
					},
					CacheSalt: testCacheSalt,
				})
				require.NoError(t, err)
				return string(res)
			}(),
			serverResponse: func() string {
				res, err := json.Marshal(anthropic.MessagesResponse{
					ID:   "msg_123",
					Type: "message",
				})
				require.NoError(t, err)
				return string(res)
			}(),
			validateResponse: func(assert *assert.Assertions, responseRecorder *httptest.ResponseRecorder) {
				assert.Equal(http.StatusOK, responseRecorder.Code)
			},
		},
		"with system prompt": {
			clientRequest: func() string {
				res, err := json.Marshal(anthropic.MessagesRequest{
					MessagesRequestPlainData: anthropic.MessagesRequestPlainData{
						Model: defaultModel,
					},
					MaxTokens: 1024,
					System:    "You are a helpful assistant.",
					Messages: []anthropic.Message{
						{Role: "user", Content: "Hello!"},
					},
					CacheSalt: testCacheSalt,
				})
				require.NoError(t, err)
				return string(res)
			}(),
			serverResponse: func() string {
				res, err := json.Marshal(anthropic.MessagesResponse{
					ID:      "msg_123",
					Type:    "message",
					Content: []anthropic.ContentBlock{{Type: "text", Text: "Hi there!"}},
					Usage:   anthropic.Usage{InputTokens: 10, OutputTokens: 5},
				})
				require.NoError(t, err)
				return string(res)
			}(),
			validateResponse: func(assert *assert.Assertions, responseRecorder *httptest.ResponseRecorder) {
				assert.Equal(http.StatusOK, responseRecorder.Code)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.serverResponse))
			}))
			defer srv.Close()

			ocspStatus, err := json.Marshal([]ocsp.StatusInfo{{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood}})
			require.NoError(err)
			ocspFile := filepath.Join(t.TempDir(), "ocsp.json")
			require.NoError(os.WriteFile(ocspFile, ocspStatus, 0o644))

			log := slog.Default()
			fwd := forwarder.New(http.DefaultClient, srv.Listener.Addr().String(), forwarder.SchemeHTTP, log)
			adapter, err := New([]string{constants.WorkloadTaskGenerate}, &stubCipher{}, ocspFile, fwd, log)
			require.NoError(err)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, anthropic.MessagesEndpoint, strings.NewReader(tc.clientRequest))
			responseRecorder := httptest.NewRecorder()

			adapter.forwardMessagesRequest(responseRecorder, request)
			tc.validateResponse(assert, responseRecorder)
		})
	}
}

func TestRegisterRoutes(t *testing.T) {
	testCases := map[string]struct {
		method       string
		path         string
		expectedCode int
	}{
		"POST messages": {
			method:       http.MethodPost,
			path:         "/v1/messages",
			expectedCode: http.StatusOK,
		},
		"GET messages returns 501": {
			method:       http.MethodGet,
			path:         "/v1/messages",
			expectedCode: http.StatusNotImplemented,
		},
		"unknown endpoint returns 501": {
			method:       http.MethodPost,
			path:         "/v1/unknown",
			expectedCode: http.StatusNotImplemented,
		},
		"root returns 501": {
			method:       http.MethodGet,
			path:         "/",
			expectedCode: http.StatusNotImplemented,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			adapter := &Adapter{
				Adapter: &inference.Adapter{
					Cipher:        &stubCipher{},
					Forwarder:     &stubForwarder{},
					WorkloadTasks: []string{constants.WorkloadTaskGenerate},
					Log:           slog.Default(),
					OCSPStatus:    []ocsp.StatusInfo{{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood}},
				},
			}

			// Build handler like the server does - middleware is applied per-route by RegisterRoutes
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "unsupported endpoint", http.StatusNotImplemented)
			})
			adapter.RegisterRoutes(mux)

			request := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, http.NoBody)
			responseRecorder := httptest.NewRecorder()

			mux.ServeHTTP(responseRecorder, request)
			assert.Equal(tc.expectedCode, responseRecorder.Code)
		})
	}
}

type stubCipher struct {
	secretMap map[string][]byte
}

func (c *stubCipher) Secret(_ context.Context, id string) ([]byte, error) {
	return c.secretMap[id], nil
}

func (c *stubCipher) NewResponseCipher() cipher.ResponseCipher {
	return c
}

func (c *stubCipher) DecryptRequest(context.Context) func(encryptedData string) (res string, err error) {
	return func(encryptedData string) (res string, err error) {
		return encryptedData, nil
	}
}

func (c *stubCipher) EncryptResponse(context.Context) func(plainData string) (string, error) {
	return func(plainData string) (res string, err error) {
		return plainData, nil
	}
}

type stubForwarder struct{}

func (f *stubForwarder) Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMapper, ...forwarder.Opts) {
}

func TestUsageExtraction(t *testing.T) {
	clientRequest := func() string {
		res, err := json.Marshal(anthropic.MessagesRequest{
			MessagesRequestPlainData: anthropic.MessagesRequestPlainData{Model: defaultModel},
			MaxTokens:                1024,
			Messages:                 []anthropic.Message{{Role: "user", Content: "Hello!"}},
			CacheSalt:                testCacheSalt,
		})
		require.NoError(t, err)
		return string(res)
	}()

	testCases := map[string]struct {
		contentType    string
		serverResponse string
		wantUsage      usage.Stats
	}{
		"unary": {
			contentType: "application/json",
			serverResponse: func() string {
				res, err := json.Marshal(anthropic.MessagesResponse{
					ID:    "msg_123",
					Type:  "message",
					Usage: anthropic.Usage{InputTokens: 10, OutputTokens: 5},
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantUsage: usage.Stats{PromptTokens: 10, CompletionTokens: 5},
		},
		"streaming": {
			contentType: "text/event-stream",
			serverResponse: strings.Join([]string{
				"event: message_start\ndata: " + `{"type":"message_start","message":{"usage":{"input_tokens":50,"output_tokens":0}}}`,
				"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
				"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":50,"output_tokens":100}}`,
				"event: message_stop\ndata: {}",
			}, "\n\n") + "\n\n",
			wantUsage: usage.Stats{PromptTokens: 50, CompletionTokens: 100},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			capture := &usageLogHandler{}
			log := slog.New(slog.NewMultiHandler(capture, slog.NewTextHandler(os.Stderr, nil)))

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = w.Write([]byte(tc.serverResponse))
			}))
			defer srv.Close()

			ocspStatus, err := json.Marshal([]ocsp.StatusInfo{{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood}})
			require.NoError(t, err)
			ocspFile := filepath.Join(t.TempDir(), "ocsp.json")
			require.NoError(t, os.WriteFile(ocspFile, ocspStatus, 0o644))

			fwd := forwarder.New(http.DefaultClient, srv.Listener.Addr().String(), forwarder.SchemeHTTP, log)
			adapter, err := New([]string{constants.WorkloadTaskGenerate}, &stubCipher{}, ocspFile, fwd, log)
			require.NoError(t, err)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, anthropic.MessagesEndpoint, strings.NewReader(clientRequest))
			responseRecorder := httptest.NewRecorder()

			adapter.forwardMessagesRequest(responseRecorder, request)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			assert.Eventually(t, func() bool {
				stats, ok := capture.latestUsage()
				return ok && stats == tc.wantUsage
			}, time.Second, 10*time.Millisecond)
		})
	}
}

// usageLogHandler captures the last usage.Stats value logged with a "usage" key.
type usageLogHandler struct {
	mu    sync.Mutex
	stats *usage.Stats
}

func (h *usageLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *usageLogHandler) Handle(_ context.Context, r slog.Record) error {
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "usage" {
			if stats, ok := a.Value.Any().(usage.Stats); ok {
				h.mu.Lock()
				h.stats = &stats
				h.mu.Unlock()
			}
		}
		return true
	})
	return nil
}

func (h *usageLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *usageLogHandler) WithGroup(string) slog.Handler      { return h }

func (h *usageLogHandler) latestUsage() (usage.Stats, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stats == nil {
		return usage.Stats{}, false
	}
	return *h.stats, true
}
