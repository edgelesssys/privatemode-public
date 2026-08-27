package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/adaptertest"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/inference"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/ocsp"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/requestid"
	"github.com/edgelesssys/continuum/internal/oss/usage"
	"github.com/edgelesssys/continuum/internal/usagecollector"
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
		serverStatus   int
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
		"list models upstream error passes through": {
			workloadTasks:  []string{constants.WorkloadTaskGenerate},
			path:           "/v1/models",
			serverResponse: `{"error":"Internal Server Error"}`,
			wantResponse:   `{"error":"Internal Server Error"}`,
			serverStatus:   http.StatusInternalServerError,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.serverStatus != 0 {
					w.WriteHeader(tc.serverStatus)
				}
				_, _ = w.Write([]byte(tc.serverResponse))
			}))
			defer srv.Close()

			ocspStatus, err := json.Marshal([]ocsp.StatusInfo{{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood}})
			require.NoError(err)
			ocspFile := filepath.Join(t.TempDir(), "ocsp.json")
			require.NoError(os.WriteFile(ocspFile, ocspStatus, 0o644))

			log := slog.Default()
			forwarder := forwarder.New(http.DefaultClient, srv.Listener.Addr().String(), forwarder.SchemeHTTP, log)
			adapter, err := New(tc.workloadTasks, nil, ocspFile, adaptertest.EncForwarder{Inner: forwarder}, &adaptertest.UsageReporter{}, log)
			require.NoError(err)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil)
			responseRecorder := httptest.NewRecorder()

			adapter.forwardModelsRequest(responseRecorder, request)
			t.Log(responseRecorder.Body.String())
			assert.JSONEq(tc.wantResponse, responseRecorder.Body.String())
			if tc.serverStatus != 0 {
				assert.Equal(tc.serverStatus, responseRecorder.Code)
			}
		})
	}
}

func TestForwardSpecificModelRequest(t *testing.T) {
	testCases := map[string]struct {
		workloadTasks  []string
		path           string
		serverResponse string
		serverStatus   int
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
		"retrieve model upstream error passes through": {
			workloadTasks:  []string{constants.WorkloadTaskGenerate},
			path:           fmt.Sprintf("/v1/models/%s", defaultModel),
			serverResponse: `{"error":"Model not found"}`,
			serverStatus:   http.StatusNotFound,
			wantResponse:   `{"error":"Model not found"}`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.serverStatus != 0 {
					w.WriteHeader(tc.serverStatus)
				}
				_, _ = w.Write([]byte(tc.serverResponse))
			}))
			defer srv.Close()

			ocspStatus, err := json.Marshal([]ocsp.StatusInfo{{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood}})
			require.NoError(err)
			ocspFile := filepath.Join(t.TempDir(), "ocsp.json")
			require.NoError(os.WriteFile(ocspFile, ocspStatus, 0o644))

			log := slog.Default()
			forwarder := forwarder.New(http.DefaultClient, srv.Listener.Addr().String(), forwarder.SchemeHTTP, log)
			adapter, err := New(tc.workloadTasks, nil, ocspFile, adaptertest.EncForwarder{Inner: forwarder}, &adaptertest.UsageReporter{}, log)
			require.NoError(err)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil)
			responseRecorder := httptest.NewRecorder()

			adapter.forwardSpecificModelRequest(responseRecorder, request)
			t.Log(responseRecorder.Body.String())
			assert.JSONEq(tc.wantResponse, responseRecorder.Body.String())
			if tc.serverStatus != 0 {
				assert.Equal(tc.serverStatus, responseRecorder.Code)
			}
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
					Model: defaultModel,
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
					Model: defaultModel,
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
					Model: defaultModel,
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
					Model: defaultModel,
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
			adapter, err := New([]string{constants.WorkloadTaskGenerate}, &adaptertest.Secrets{}, ocspFile, adaptertest.EncForwarder{Inner: forwarder}, &adaptertest.UsageReporter{}, log)
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
			adapter, err := New([]string{constants.WorkloadTaskGenerate}, &adaptertest.Secrets{}, ocspFile, adaptertest.EncForwarder{Inner: forwarder}, &adaptertest.UsageReporter{}, log)
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
			Secrets:       &adaptertest.Secrets{},
			EncForwarder:  adaptertest.EncForwarder{},
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

func stubRequestMutator(_ *http.Request) error { return nil }

func TestUsageExtraction(t *testing.T) {
	clientRequest := func() string {
		res, err := json.Marshal(openai.ChatRequest{
			Model:     defaultModel,
			Messages:  []openai.Message{{Role: "user", Content: "Hello!"}},
			CacheSalt: strings.Repeat("a", 32),
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
				res, err := json.Marshal(openai.ChatResponse{
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
		"unary with cached tokens": {
			contentType: "application/json",
			serverResponse: func() string {
				res, err := json.Marshal(openai.ChatResponse{
					Usage: openai.Usage{
						PromptTokens:        50,
						CompletionTokens:    100,
						TotalTokens:         150,
						PromptTokensDetails: &openai.PromptTokensDetails{CachedTokens: 20},
					},
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantUsage: usage.Stats{PromptTokens: 30, CachedPromptTokens: 20, CompletionTokens: 100},
		},
		"unary with cached tokens exceeding prompt tokens": {
			contentType: "application/json",
			serverResponse: func() string {
				res, err := json.Marshal(openai.ChatResponse{
					Usage: openai.Usage{
						PromptTokens:        50,
						CompletionTokens:    100,
						TotalTokens:         150,
						PromptTokensDetails: &openai.PromptTokensDetails{CachedTokens: 80},
					},
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantUsage: usage.Stats{PromptTokens: 0, CachedPromptTokens: 80, CompletionTokens: 100},
		},
		"streaming with cached tokens": {
			contentType: "text/event-stream",
			serverResponse: strings.Join([]string{
				`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
				`data: {"choices":[],"usage":{"prompt_tokens":50,"completion_tokens":100,"total_tokens":150,"prompt_tokens_details":{"cached_tokens":20}}}`,
				"data: [DONE]",
			}, "\n\n") + "\n\n",
			wantUsage: usage.Stats{PromptTokens: 30, CachedPromptTokens: 20, CompletionTokens: 100},
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
			fwd := forwarder.New(http.DefaultClient, srv.Listener.Addr().String(), forwarder.SchemeHTTP, slog.Default())
			adapter, err := New([]string{constants.WorkloadTaskGenerate}, &adaptertest.Secrets{}, ocspFile, adaptertest.EncForwarder{Inner: fwd}, reporter, slog.Default())
			require.NoError(t, err)

			ctx := requestid.WithContext(t.Context(), "test-request-id")
			request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/chat/completions", strings.NewReader(clientRequest))
			request.Header.Set(usagecollector.PrivatemodeUsageIDHeader, "test-usage-id")
			responseRecorder := httptest.NewRecorder()

			adapter.forwardChatCompletionsRequest(responseRecorder, request)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			assert.Eventually(t, func() bool {
				report, ok := reporter.LatestReport()
				return ok && report.Stats == tc.wantUsage
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
		"unary with usage seconds": {
			contentType:    "application/json",
			serverResponse: `{"text":"hello","usage":{"type":"duration","seconds":25}}`,
			wantUsage:      usage.Stats{AudioSeconds: 25},
		},
		"unary with top-level duration": {
			contentType:    "application/json",
			serverResponse: `{"text":"hello","duration":42.3}`,
			wantUsage:      usage.Stats{AudioSeconds: 43},
		},
		"unary with fractional usage seconds": {
			contentType:    "application/json",
			serverResponse: `{"text":"hello","usage":{"type":"duration","seconds":25.4}}`,
			wantUsage:      usage.Stats{AudioSeconds: 26},
		},
		"unary with duration and usage.seconds uses max": {
			contentType:    "application/json",
			serverResponse: `{"text":"hello","duration":30,"usage":{"type":"duration","seconds":25}}`,
			wantUsage:      usage.Stats{AudioSeconds: 30},
		},
		"streaming whisper": {
			contentType: "text/event-stream",
			serverResponse: strings.Join([]string{
				`data: {"model":"openai/whisper-large-v3","choices":[{"delta":{"content":"Hello"}}],"usage":{"prompt_tokens":4000,"completion_tokens":1,"total_tokens":4001}}`,
				`data: [DONE]`,
			}, "\n\n") + "\n\n",
			wantUsage: usage.Stats{AudioSeconds: 40},
		},
		"streaming voxtral": {
			contentType: "text/event-stream",
			serverResponse: strings.Join([]string{
				`data: {"model":"voxtral-mini-3b","choices":[{"delta":{"content":"Hello"}}],"usage":{"prompt_tokens":1073,"completion_tokens":1,"total_tokens":1074}}`,
				`data: [DONE]`,
			}, "\n\n") + "\n\n",
			wantUsage: usage.Stats{AudioSeconds: 30},
		},
		"streaming unknown model uses voxtral fallback": {
			contentType: "text/event-stream",
			serverResponse: strings.Join([]string{
				`data: {"model":"some-unknown-model","choices":[{"delta":{"content":"Hello"}}],"usage":{"prompt_tokens":1073,"completion_tokens":1,"total_tokens":1074}}`,
				`data: [DONE]`,
			}, "\n\n") + "\n\n",
			wantUsage: usage.Stats{AudioSeconds: 30},
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
			fwd := forwarder.New(http.DefaultClient, srv.Listener.Addr().String(), forwarder.SchemeHTTP, slog.Default())
			adapter, err := New([]string{constants.WorkloadTaskGenerate}, &adaptertest.Secrets{}, ocspFile, adaptertest.EncForwarder{Inner: fwd}, reporter, slog.Default())
			require.NoError(t, err)

			// Build multipart form request with a model field.
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.WriteField("model", defaultModel))
			require.NoError(t, writer.Close())

			ctx := requestid.WithContext(t.Context(), "test-request-id")
			request := httptest.NewRequestWithContext(ctx, http.MethodPost, openai.TranscriptionsEndpoint, &body)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			request.Header.Set(usagecollector.PrivatemodeUsageIDHeader, "test-usage-id")
			responseRecorder := httptest.NewRecorder()

			adapter.forwardTranscriptionsRequest(responseRecorder, request)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			assert.Eventually(t, func() bool {
				report, ok := reporter.LatestReport()
				return ok && report.Stats == tc.wantUsage
			}, time.Second, 10*time.Millisecond)
		})
	}
}

func TestTranscriptionDurationCompat(t *testing.T) {
	const numberResponse = `{"text":"hello","duration":42.3}`

	testCases := map[string]struct {
		clientVersion  string
		serverResponse string
		wantResponse   string
	}{
		"client without version header gets the string": {
			serverResponse: numberResponse,
			wantResponse:   `{"text":"hello","duration":"42.3"}`,
		},
		"old client gets the string": {
			clientVersion:  "v1.46.0",
			serverResponse: numberResponse,
			wantResponse:   `{"text":"hello","duration":"42.3"}`,
		},
		"client of the announcing release gets the string": {
			clientVersion:  "v1.54.0",
			serverResponse: numberResponse,
			wantResponse:   `{"text":"hello","duration":"42.3"}`,
		},
		"pre-release client of the announcing release gets the string": {
			clientVersion:  "v1.54.0-pre.0.20260824153000-abcdef123456",
			serverResponse: numberResponse,
			wantResponse:   `{"text":"hello","duration":"42.3"}`,
		},
		"client after the announcing release gets the number": {
			clientVersion:  "v1.55.0",
			serverResponse: numberResponse,
			wantResponse:   numberResponse,
		},
		"integer duration keeps its digits": {
			clientVersion:  "v1.54.0",
			serverResponse: `{"text":"hello","duration":42}`,
			wantResponse:   `{"text":"hello","duration":"42"}`,
		},
		"non-numeric duration is left alone": {
			clientVersion:  "v1.54.0",
			serverResponse: `{"text":"hello","duration":"42.3"}`,
			wantResponse:   `{"text":"hello","duration":"42.3"}`,
		},
		"response without duration is unchanged": {
			clientVersion:  "v1.54.0",
			serverResponse: `{"text":"hello","usage":{"type":"duration","seconds":25}}`,
			wantResponse:   `{"text":"hello","usage":{"type":"duration","seconds":25}}`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.serverResponse))
			}))
			defer srv.Close()

			ocspStatus, err := json.Marshal([]ocsp.StatusInfo{{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood}})
			require.NoError(t, err)
			ocspFile := filepath.Join(t.TempDir(), "ocsp.json")
			require.NoError(t, os.WriteFile(ocspFile, ocspStatus, 0o644))

			fwd := forwarder.New(http.DefaultClient, srv.Listener.Addr().String(), forwarder.SchemeHTTP, slog.Default())
			adapter, err := New([]string{constants.WorkloadTaskGenerate}, &adaptertest.Secrets{}, ocspFile, adaptertest.EncForwarder{Inner: fwd}, &adaptertest.UsageReporter{}, slog.Default())
			require.NoError(t, err)

			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.WriteField("model", defaultModel))
			require.NoError(t, writer.Close())

			ctx := requestid.WithContext(t.Context(), "test-request-id")
			request := httptest.NewRequestWithContext(ctx, http.MethodPost, openai.TranscriptionsEndpoint, &body)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			request.Header.Set(constants.PrivatemodeVersionHeader, tc.clientVersion)
			responseRecorder := httptest.NewRecorder()

			adapter.forwardTranscriptionsRequest(responseRecorder, request)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)
			assert.Equal(t, tc.wantResponse, responseRecorder.Body.String())
		})
	}
}
