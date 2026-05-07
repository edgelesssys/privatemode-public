package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime/multipart"
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
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/ocsp"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const defaultModel = "some-model"

// TestSetBytes checks that a string is correctly inserted into a JSON string.
func TestSetBytes(t *testing.T) {
	replace := `demo-app:3a71ea7448791716e325146b:a03acc195834a9822d676e797381c035418dde3539cf46ae61d0ef2ff59b81f1d7d05cc5d8b79cf2ec08c9ce147c90c3`
	original := `{"model": "model","messages": [{"role": "user", "content": "Hi"}],"temperature": 1}`

	res, err := sjson.SetBytes([]byte(original), "messages", []byte(replace))
	assert.NoError(t, err)
	assert.Equal(t, replace, gjson.GetBytes(res, "messages").String())
}

// TestSetRawBytes checks that a marshalled JSON is correctly inserted into a JSON string.
func TestSetRawBytes(t *testing.T) {
	replace := `[{"role": "user", "content": "Write a haiku about the dust on my floor."}]`
	original := `{"model": "model","messages": [{"role": "user", "content": "Hi"}],"temperature": 1}`

	res, err := sjson.SetRawBytes([]byte(original), "messages", []byte(replace))
	assert.NoError(t, err)
	assert.Equal(t, replace, gjson.GetBytes(res, "messages").String())
}

func TestForwardModelsRequest(t *testing.T) {
	testCases := map[string]struct {
		workloadTasks  []string
		path           string
		serverResponse string
		wantResponse   string
	}{
		"list models": {
			workloadTasks: []string{constants.WorkloadTaskGenerate},
			path:          "/v1/models",
			serverResponse: func() string {
				res, err := json.Marshal(openai.ModelsResponse{
					Object: "list",
					Data: []openai.Model{
						{
							ID:     defaultModel,
							Object: "model",
						},
					},
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantResponse: func() string {
				res, err := json.Marshal(openai.ModelsResponse{
					Object: "list",
					Data: []openai.Model{
						{
							ID:     defaultModel,
							Object: "model",
							Tasks:  []string{constants.WorkloadTaskGenerate},
						},
					},
				})
				require.NoError(t, err)
				return string(res)
			}(),
		},
		"list models with multiple tasks": {
			workloadTasks: []string{constants.WorkloadTaskGenerate, "custom-task"},
			path:          "/v1/models",
			serverResponse: func() string {
				res, err := json.Marshal(openai.ModelsResponse{
					Object: "list",
					Data: []openai.Model{
						{
							ID:     defaultModel,
							Object: "model",
						},
						{
							ID:     "llama3",
							Object: "model",
						},
					},
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantResponse: func() string {
				res, err := json.Marshal(openai.ModelsResponse{
					Object: "list",
					Data: []openai.Model{
						{
							ID:     defaultModel,
							Object: "model",
							Tasks:  []string{constants.WorkloadTaskGenerate, "custom-task"},
						},
						{
							ID:     "llama3",
							Object: "model",
							Tasks:  []string{constants.WorkloadTaskGenerate, "custom-task"},
						},
					},
				})
				require.NoError(t, err)
				return string(res)
			}(),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.serverResponse))
			}))
			defer srv.Close()

			ocspStatus, err := json.Marshal([]ocsp.StatusInfo{{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood}})
			require.NoError(err)
			ocspFile := filepath.Join(t.TempDir(), "ocsp.json")
			require.NoError(os.WriteFile(ocspFile, ocspStatus, 0o644))

			log := slog.Default()
			forwarder := forwarder.New(http.DefaultClient, srv.Listener.Addr().String(), forwarder.SchemeHTTP, log)
			adapter, err := New(tc.workloadTasks, nil, ocspFile, forwarder, log)
			require.NoError(err)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil)
			responseRecorder := httptest.NewRecorder()

			adapter.forwardModelsRequest(responseRecorder, request)
			t.Log(responseRecorder.Body.String())
			assert.JSONEq(tc.wantResponse, responseRecorder.Body.String())
		})
	}
}

func TestForwardSpecificModelRequest(t *testing.T) {
	testCases := map[string]struct {
		workloadTasks  []string
		path           string
		serverResponse string
		wantResponse   string
	}{
		"retrieve model": {
			workloadTasks: []string{constants.WorkloadTaskGenerate},
			path:          fmt.Sprintf("/v1/models/%s", defaultModel),
			serverResponse: func() string {
				res, err := json.Marshal(openai.Model{
					ID:     defaultModel,
					Object: "model",
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantResponse: func() string {
				res, err := json.Marshal(openai.Model{
					ID:     defaultModel,
					Object: "model",
					Tasks:  []string{constants.WorkloadTaskGenerate},
				})
				require.NoError(t, err)
				return string(res)
			}(),
		},
		"retrieve model with multiple tasks": {
			workloadTasks: []string{constants.WorkloadTaskGenerate, constants.WorkloadTaskToolCalling},
			path:          fmt.Sprintf("/v1/models/%s", defaultModel),
			serverResponse: func() string {
				res, err := json.Marshal(openai.Model{
					ID:     defaultModel,
					Object: "model",
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantResponse: func() string {
				res, err := json.Marshal(openai.Model{
					ID:     defaultModel,
					Object: "model",
					Tasks:  []string{constants.WorkloadTaskGenerate, constants.WorkloadTaskToolCalling},
				})
				require.NoError(t, err)
				return string(res)
			}(),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.serverResponse))
			}))
			defer srv.Close()

			ocspStatus, err := json.Marshal([]ocsp.StatusInfo{{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood}})
			require.NoError(err)
			ocspFile := filepath.Join(t.TempDir(), "ocsp.json")
			require.NoError(os.WriteFile(ocspFile, ocspStatus, 0o644))

			log := slog.Default()
			forwarder := forwarder.New(http.DefaultClient, srv.Listener.Addr().String(), forwarder.SchemeHTTP, log)
			adapter, err := New(tc.workloadTasks, nil, ocspFile, forwarder, log)
			require.NoError(err)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil)
			responseRecorder := httptest.NewRecorder()

			adapter.forwardSpecificModelRequest(responseRecorder, request)
			t.Log(responseRecorder.Body.String())
			assert.JSONEq(tc.wantResponse, responseRecorder.Body.String())
		})
	}
}

func TestForwardChatCompletionsRequest(t *testing.T) {
	checkSuccessfulResponse := func(assert *assert.Assertions, responseRecorder *httptest.ResponseRecorder) {
		assert.Equal(http.StatusOK, responseRecorder.Code)
	}

	testCases := map[string]struct {
		clientRequest    string
		validateResponse func(assert *assert.Assertions, responseRecorder *httptest.ResponseRecorder)
	}{
		"success": {
			clientRequest: func() string {
				res, err := json.Marshal(openai.ChatRequest{
					ChatRequestPlainData: openai.ChatRequestPlainData{
						Model: defaultModel,
					},
					Messages: []openai.Message{
						{
							Role:    "user",
							Content: "Tell me a joke!",
						},
					},
					CacheSalt: strings.Repeat("a", 32),
				})
				require.NoError(t, err)
				return string(res)
			}(),
			validateResponse: checkSuccessfulResponse,
		},
		"HTTPS image URL accepted": {
			clientRequest: func() string {
				res, err := json.Marshal(openai.ChatRequest{
					ChatRequestPlainData: openai.ChatRequestPlainData{
						Model: defaultModel,
					},
					Messages: []openai.Message{
						{
							Role: "user",
							Content: []map[string]any{
								{
									"type":      "image_url",
									"image_url": map[string]string{"url": "https://example.com/image.jpg"},
								},
							},
						},
					},
					CacheSalt: strings.Repeat("a", 32),
				})
				require.NoError(t, err)
				return string(res)
			}(),
			validateResponse: checkSuccessfulResponse,
		},
		"data image URL accepted": {
			clientRequest: func() string {
				res, err := json.Marshal(openai.ChatRequest{
					ChatRequestPlainData: openai.ChatRequestPlainData{
						Model: defaultModel,
					},
					Messages: []openai.Message{
						{
							Role: "user",
							Content: []map[string]any{
								{
									"type":      "image_url",
									"image_url": map[string]string{"url": "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAAYEBQYFBAYGBQYHBwYIChAKCgkJChQODwwQFxQYGBcUFhYaHSUfGhsjHBYWICwgIyYnKSopGR8tMC0oMCUoKSj/2wBDAQcHBwoIChMKChMoGhYa"},
								},
							},
						},
					},
					CacheSalt: strings.Repeat("a", 32),
				})
				require.NoError(t, err)
				return string(res)
			}(),
			validateResponse: checkSuccessfulResponse,
		},
		"non-HTTPS image URL rejected": {
			clientRequest: func() string {
				res, err := json.Marshal(openai.ChatRequest{
					ChatRequestPlainData: openai.ChatRequestPlainData{
						Model: defaultModel,
					},
					Messages: []openai.Message{
						{
							Role: "user",
							Content: []map[string]any{
								{
									"type":      "image_url",
									"image_url": map[string]string{"url": "http://example.com/image.jpg"},
								},
							},
						},
					},
					CacheSalt: strings.Repeat("a", 32),
				})
				require.NoError(t, err)
				return string(res)
			}(),
			validateResponse: func(assert *assert.Assertions, responseRecorder *httptest.ResponseRecorder) {
				assert.Equal(http.StatusInternalServerError, responseRecorder.Code)
				assert.Contains(responseRecorder.Body.String(), "non-HTTPS and non-data image URL \\\"http://example.com/image.jpg\\\" is insecure")
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("{}"))
			}))
			defer srv.Close()

			ocspStatus, err := json.Marshal([]ocsp.StatusInfo{{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood}})
			require.NoError(err)
			ocspFile := filepath.Join(t.TempDir(), "ocsp.json")
			require.NoError(os.WriteFile(ocspFile, ocspStatus, 0o644))

			log := slog.Default()
			forwarder := forwarder.New(http.DefaultClient, srv.Listener.Addr().String(), forwarder.SchemeHTTP, log)
			adapter, err := New([]string{constants.WorkloadTaskGenerate}, &stubCipher{}, ocspFile, forwarder, log)
			require.NoError(err)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.clientRequest))
			responseRecorder := httptest.NewRecorder()

			adapter.forwardChatCompletionsRequest(responseRecorder, request)
			tc.validateResponse(assert, responseRecorder)
		})
	}
}

func TestForwardChatCompletionsRequestDuplicatesReasoningContent(t *testing.T) {
	testCases := map[string]struct {
		serverBody   string
		assertBody   func(*testing.T, string)
		serverHeader func(http.Header)
	}{
		"non-streaming": {
			serverBody: `{"id":"chatcmpl-123","choices":[{"index":0,"message":{"role":"assistant","content":"answer","reasoning":"because"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
			assertBody: func(t *testing.T, body string) {
				assert.JSONEq(t, `{"id":"chatcmpl-123","choices":[{"index":0,"message":{"role":"assistant","content":"answer","reasoning":"because","reasoning_content":"because"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`, body)
			},
		},
		"streaming": {
			serverBody: strings.Join([]string{
				`data: {"id":"chatcmpl-123","choices":[{"index":0,"delta":{"content":"answer","reasoning":"because"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"),
			serverHeader: func(header http.Header) {
				header.Set("Content-Type", "text/event-stream")
			},
			assertBody: func(t *testing.T, body string) {
				assert.Contains(t, body, `"reasoning":"because"`)
				assert.Contains(t, body, `"reasoning_content":"because"`)
				assert.Contains(t, body, `data: [DONE]`)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.serverHeader != nil {
					tc.serverHeader(w.Header())
				}
				_, _ = w.Write([]byte(tc.serverBody))
			}))
			defer srv.Close()

			ocspStatus, err := json.Marshal([]ocsp.StatusInfo{{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood}})
			require.NoError(err)
			ocspFile := filepath.Join(t.TempDir(), "ocsp.json")
			require.NoError(os.WriteFile(ocspFile, ocspStatus, 0o644))

			log := slog.Default()
			forwarder := forwarder.New(http.DefaultClient, srv.Listener.Addr().String(), forwarder.SchemeHTTP, log)
			adapter, err := New([]string{constants.WorkloadTaskGenerate}, &stubCipher{}, ocspFile, forwarder, log)
			require.NoError(err)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"some-model","messages":[{"role":"user","content":"hello"}],"cache_salt":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
			responseRecorder := httptest.NewRecorder()

			adapter.forwardChatCompletionsRequest(responseRecorder, request)
			require.Equal(http.StatusOK, responseRecorder.Code)
			tc.assertBody(t, responseRecorder.Body.String())
		})
	}
}

// TestModelsEndpointExcludedFromOCSP verifies that the models endpoint is excluded from OCSP verification.
// This is an OpenAI-specific behavior since /v1/models is used for health checks and shouldn't require GPU attestation.
func TestModelsEndpointExcludedFromOCSP(t *testing.T) {
	assert := assert.New(t)

	// Create adapter with bad OCSP status - would normally fail verification
	a := &Adapter{
		Adapter: &inference.Adapter{
			Cipher:        &stubCipher{},
			Forwarder:     &stubForwarder{},
			WorkloadTasks: []string{constants.WorkloadTaskGenerate},
			Log:           slog.Default(),
			OCSPStatus:    []ocsp.StatusInfo{{GPU: ocsp.StatusUnknown, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood}},
		},
		mutators: openai.DefaultRequestMutators{
			CacheSaltInjector:     stubRequestMutator,
			CacheSaltValidator:    stubRequestMutator,
			MediaContentValidator: stubRequestMutator,
		},
	}

	// Build handler like the server does - middleware is applied per-route by RegisterRoutes
	mux := http.NewServeMux()
	a.RegisterRoutes(mux)

	// Models endpoint should succeed despite bad OCSP status (not wrapped with OCSP middleware)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, openai.ModelsEndpoint, http.NoBody)
	responseRecorder := httptest.NewRecorder()
	mux.ServeHTTP(responseRecorder, request)
	assert.Equal(http.StatusOK, responseRecorder.Code)

	// Other endpoints should fail with bad OCSP status (wrapped with OCSP middleware)
	request = httptest.NewRequestWithContext(t.Context(), http.MethodPost, openai.ChatCompletionsEndpoint, http.NoBody)
	responseRecorder = httptest.NewRecorder()
	mux.ServeHTTP(responseRecorder, request)
	assert.Equal(http.StatusInternalServerError, responseRecorder.Code)
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

func stubRequestMutator(_ *http.Request) error { return nil }

func TestUsageExtraction(t *testing.T) {
	clientRequest := func() string {
		res, err := json.Marshal(openai.ChatRequest{
			ChatRequestPlainData: openai.ChatRequestPlainData{Model: defaultModel},
			Messages:             []openai.Message{{Role: "user", Content: "Hello!"}},
			CacheSalt:            strings.Repeat("a", 32),
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
				res, err := json.Marshal(openai.EncryptedChatResponse{
					Usage: openai.Usage{
						PromptTokens:     50,
						CompletionTokens: 100,
						TotalTokens:      150,
					},
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantUsage: usage.Stats{PromptTokens: 50, CompletionTokens: 100},
		},
		"streaming": {
			contentType: "text/event-stream",
			serverResponse: strings.Join([]string{
				`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
				`data: {"choices":[],"usage":{"prompt_tokens":50,"completion_tokens":100,"total_tokens":150}}`,
				"data: [DONE]",
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

			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", strings.NewReader(clientRequest))
			responseRecorder := httptest.NewRecorder()

			adapter.forwardChatCompletionsRequest(responseRecorder, request)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			assert.Eventually(t, func() bool {
				stats, ok := capture.latestUsage()
				return ok && stats == tc.wantUsage
			}, time.Second, 10*time.Millisecond)
		})
	}
}

func TestTranscriptionUsageExtraction(t *testing.T) {
	testCases := map[string]struct {
		contentType    string
		serverResponse string
		wantUsage      usage.Stats
	}{
		"unary": {
			contentType:    "application/json",
			serverResponse: `{"text":"hello","duration":"34","usage":{"type":"duration","seconds":35}}`,
			wantUsage:      usage.Stats{AudioSeconds: 35},
		},
		"streaming returns zero usage because vLLM uses token-based usage in streams": {
			contentType: "text/event-stream",
			serverResponse: strings.Join([]string{
				`data: {"choices":[{"delta":{"content":"Hello"}}],"usage":{"prompt_tokens":100,"total_tokens":101,"completion_tokens":1}}`,
				`data: {"choices":[{"delta":{"content":" world"}}],"usage":{"prompt_tokens":100,"total_tokens":102,"completion_tokens":2}}`,
				"data: [DONE]",
			}, "\n\n") + "\n\n",
			wantUsage: usage.Stats{
				PromptTokens:     100,
				CompletionTokens: 2,
			},
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

			// Build multipart form request with a model field.
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.WriteField("model", defaultModel))
			require.NoError(t, writer.Close())

			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, openai.TranscriptionsEndpoint, &body)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			responseRecorder := httptest.NewRecorder()

			adapter.forwardTranscriptionsRequest(responseRecorder, request)

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
