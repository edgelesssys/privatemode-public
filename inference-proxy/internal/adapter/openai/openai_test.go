package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/inference"
	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/ocsp"
	"github.com/edgelesssys/continuum/internal/oss/openai"
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
