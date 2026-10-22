package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/adaptertest"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/inference"
	"github.com/edgelesssys/continuum/internal/oss/anthropic"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/ocsp"
	"github.com/edgelesssys/continuum/internal/oss/requestid"
	"github.com/edgelesssys/continuum/internal/oss/sse"
	"github.com/edgelesssys/continuum/internal/oss/usage"
	"github.com/edgelesssys/continuum/internal/usagecollector"
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
					Model:     defaultModel,
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
					Model:     defaultModel,
					Stream:    true,
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
					Model:     defaultModel,
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
			fwd := forwarder.New(http.DefaultClient, srv.Listener.Addr().String(), forwarder.SchemeHTTP, forwarder.UpstreamModel, log)
			adapter, err := New([]string{constants.WorkloadTaskGenerate}, &adaptertest.Secrets{}, ocspFile, adaptertest.EncForwarder{Inner: fwd}, &adaptertest.UsageReporter{}, log)
			require.NoError(err)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, constants.AnthropicMessagesEndpoint, strings.NewReader(tc.clientRequest))
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
			path:         constants.AnthropicMessagesEndpoint,
			expectedCode: http.StatusOK,
		},
		"GET messages returns 501": {
			method:       http.MethodGet,
			path:         constants.AnthropicMessagesEndpoint,
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
					Secrets:       &adaptertest.Secrets{},
					EncForwarder:  adaptertest.EncForwarder{},
					UsageReporter: &adaptertest.UsageReporter{},
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

func TestUsageExtraction(t *testing.T) {
	clientRequest := func() string {
		res, err := json.Marshal(anthropic.MessagesRequest{
			Model:     defaultModel,
			MaxTokens: 1024,
			Messages:  []anthropic.Message{{Role: "user", Content: "Hello!"}},
			CacheSalt: testCacheSalt,
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
		"unary with cached tokens": {
			contentType: "application/json",
			serverResponse: func() string {
				res, err := json.Marshal(anthropic.MessagesResponse{
					ID:   "msg_123",
					Type: "message",
					Usage: anthropic.Usage{
						InputTokens:              10,
						OutputTokens:             3,
						CacheReadInputTokens:     20,
						CacheCreationInputTokens: 5,
					},
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantUsage: usage.Stats{PromptTokens: 15, CachedPromptTokens: 20, CompletionTokens: 3},
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
		"streaming with cached tokens": {
			contentType: "text/event-stream",
			serverResponse: strings.Join([]string{
				"event: message_start\ndata: " + `{"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":0,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}}}`,
				"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
				"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}}`,
				"event: message_stop\ndata: {}",
			}, "\n\n") + "\n\n",
			wantUsage: usage.Stats{PromptTokens: 15, CachedPromptTokens: 20, CompletionTokens: 3},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = w.Write([]byte(tc.serverResponse))
			}))
			defer srv.Close()

			ocspStatus, err := json.Marshal([]ocsp.StatusInfo{{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood}})
			require.NoError(t, err)
			ocspFile := filepath.Join(t.TempDir(), "ocsp.json")
			require.NoError(t, os.WriteFile(ocspFile, ocspStatus, 0o644))

			reporter := &adaptertest.UsageReporter{}
			fwd := forwarder.New(http.DefaultClient, srv.Listener.Addr().String(), forwarder.SchemeHTTP, forwarder.UpstreamModel, slog.Default())
			adapter, err := New([]string{constants.WorkloadTaskGenerate}, &adaptertest.Secrets{}, ocspFile, adaptertest.EncForwarder{Inner: fwd}, reporter, slog.Default())
			require.NoError(t, err)

			ctx := requestid.WithContext(t.Context(), "test-request-id")
			request := httptest.NewRequestWithContext(ctx, http.MethodPost, constants.AnthropicMessagesEndpoint, strings.NewReader(clientRequest))
			request.Header.Set(usagecollector.PrivatemodeUsageIDHeader, "test-usage-id")
			responseRecorder := httptest.NewRecorder()

			adapter.forwardMessagesRequest(responseRecorder, request)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			assert.Eventually(t, func() bool {
				report, ok := reporter.LatestReport()
				return ok && report.Stats == tc.wantUsage
			}, time.Second, 10*time.Millisecond)
		})
	}
}

func TestExtractAnthropicUsageStream(t *testing.T) {
	extract := extractAnthropicUsageStream()
	testCases := []struct {
		name string
		body string
		want usage.Stats
	}{
		{
			name: "initial usage",
			body: `{"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":0,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}}}`,
			want: usage.Stats{PromptTokens: 15, CachedPromptTokens: 20},
		},
		{
			name: "output only",
			body: `{"type":"message_delta","usage":{"output_tokens":10}}`,
			want: usage.Stats{PromptTokens: 15, CachedPromptTokens: 20, CompletionTokens: 10},
		},
		{
			name: "cumulative output",
			body: `{"type":"message_delta","usage":{"output_tokens":20}}`,
			want: usage.Stats{PromptTokens: 15, CachedPromptTokens: 20, CompletionTokens: 20},
		},
		{
			name: "input only preserves output and cache creation",
			body: `{"type":"message_delta","usage":{"input_tokens":12}}`,
			want: usage.Stats{PromptTokens: 17, CachedPromptTokens: 20, CompletionTokens: 20},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extract([]byte(tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	for _, body := range []string{
		`{"type":"content_block_delta"}`,
		`{"type":"message_stop"}`,
		`{"type":"message_delta"}`,
		`{"type":"message_delta","usage":null}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":null}`,
	} {
		_, err := extract([]byte(body))
		require.ErrorIs(t, err, inference.ErrNoUsage)
	}
	for _, body := range []string{
		`not json`,
		`{"type":"message_delta","usage":{"input_tokens":999,"output_tokens":"invalid"}}`,
	} {
		_, err := extract([]byte(body))
		require.Error(t, err)
	}
	got, err := extract([]byte(`{"type":"message_delta","usage":{"output_tokens":30}}`))
	require.NoError(t, err)
	assert.Equal(t, usage.Stats{PromptTokens: 17, CachedPromptTokens: 20, CompletionTokens: 30}, got)

	other := extractAnthropicUsageStream()
	got, err = other([]byte(`{"type":"message_delta","usage":{"output_tokens":1}}`))
	require.NoError(t, err)
	assert.Equal(t, usage.Stats{CompletionTokens: 1}, got)
}

func TestExtractAnthropicUsageStreamFinalUsage(t *testing.T) {
	testCases := map[string]struct {
		initialUsage string
		finalUsage   string
		want         usage.Stats
	}{
		"final usage splits input into regular and cached tokens": {
			initialUsage: `{"input_tokens":35,"output_tokens":0}`,
			finalUsage:   `{"input_tokens":10,"output_tokens":25,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}`,
			want:         usage.Stats{PromptTokens: 15, CachedPromptTokens: 20, CompletionTokens: 25},
		},
		"final usage can contain only cached input tokens": {
			initialUsage: `{"input_tokens":35,"output_tokens":0}`,
			finalUsage:   `{"input_tokens":0,"output_tokens":25,"cache_read_input_tokens":35,"cache_creation_input_tokens":0}`,
			want:         usage.Stats{CachedPromptTokens: 35, CompletionTokens: 25},
		},
		"final omitted fields do not retain earlier counts": {
			initialUsage: `{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}`,
			finalUsage:   `{"output_tokens":25}`,
			want:         usage.Stats{CompletionTokens: 25},
		},
		"final usage without earlier events": {
			finalUsage: `{"input_tokens":10,"output_tokens":25,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}`,
			want:       usage.Stats{PromptTokens: 15, CachedPromptTokens: 20, CompletionTokens: 25},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			extract := extractAnthropicUsageStream()
			if tc.initialUsage != "" {
				_, err := extract([]byte(`{"type":"message_start","message":{"usage":` + tc.initialUsage + `}}`))
				require.NoError(t, err)
			}

			got, err := extract([]byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":` + tc.finalUsage + `}`))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAnthropicUsageStreamCancelled(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":0,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}}}`,
		`{"type":"message_delta","usage":{"output_tokens":10}}`,
		`{"type":"message_delta","usage":{"output_tokens":20}}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"More text"}}`,
	}
	for _, count := range []int{1, 2, 4} {
		stream := "data: " + strings.Join(events[:count], "\n\ndata: ") + "\n\n"
		source := io.MultiReader(strings.NewReader(stream), iotest.ErrReader(context.Canceled))
		reader := inference.NewUsageSSEReader(sse.NewEventReader(source, constants.MaxSSEEventBytes), extractAnthropicUsageStream())
		for range count {
			_, err := reader.Next()
			require.NoError(t, err)
		}
		_, err := reader.Next()
		require.ErrorIs(t, err, context.Canceled)
		want := usage.Stats{PromptTokens: 15, CachedPromptTokens: 20}
		if count > 1 {
			want.CompletionTokens = 10
		}
		if count > 2 {
			want.CompletionTokens = 20
		}
		assert.Equal(t, want, reader.LatestUsage())
	}
}
