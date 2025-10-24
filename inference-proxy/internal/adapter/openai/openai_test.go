package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/internal/gpl/ocsp"
	"github.com/edgelesssys/continuum/internal/gpl/ocspheader"
	"github.com/edgelesssys/continuum/internal/gpl/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

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
							ID:     "llama-3.3-70b",
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
							ID:     "llama-3.3-70b",
							Object: "model",
							Tasks:  []string{constants.WorkloadTaskGenerate},
						},
					},
				})
				require.NoError(t, err)
				return string(res)
			}(),
		},
		"retrieve model": {
			workloadTasks: []string{constants.WorkloadTaskGenerate},
			path:          "/v1/models/llama-3.3-70b",
			serverResponse: func() string {
				res, err := json.Marshal(openai.Model{
					ID:     "llama-3.3-70b",
					Object: "model",
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantResponse: func() string {
				res, err := json.Marshal(openai.Model{
					ID:     "llama-3.3-70b",
					Object: "model",
					Tasks:  []string{constants.WorkloadTaskGenerate},
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
							ID:     "llama-3.3-70b",
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
							ID:     "llama-3.3-70b",
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
		"retrieve model with multiple tasks": {
			workloadTasks: []string{constants.WorkloadTaskGenerate, constants.WorkloadTaskToolCalling},
			path:          "/v1/models/llama-3.3-70b",
			serverResponse: func() string {
				res, err := json.Marshal(openai.Model{
					ID:     "llama-3.3-70b",
					Object: "model",
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantResponse: func() string {
				res, err := json.Marshal(openai.Model{
					ID:     "llama-3.3-70b",
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

			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			responseRecorder := httptest.NewRecorder()

			adapter.forwardModelsRequest(responseRecorder, request)
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
						Model: constants.DefaultTextgenModel,
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
						Model: constants.DefaultTextgenModel,
					},
					Messages: []openai.Message{
						{
							Role: "user",
							Content: []map[string]string{
								{
									"type":      "input_image",
									"image_url": "https://example.com/image.jpg",
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
						Model: constants.DefaultTextgenModel,
					},
					Messages: []openai.Message{
						{
							Role: "user",
							Content: []map[string]string{
								{
									"type":      "input_image",
									"image_url": "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAAYEBQYFBAYGBQYHBwYIChAKCgkJChQODwwQFxQYGBcUFhYaHSUfGhsjHBYWICwgIyYnKSopGR8tMC0oMCUoKSj/2wBDAQcHBwoIChMKChMoGhYa",
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
						Model: constants.DefaultTextgenModel,
					},
					Messages: []openai.Message{
						{
							Role: "user",
							Content: []map[string]string{
								{
									"type":      "input_image",
									"image_url": "http://example.com/image.jpg",
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

			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.clientRequest))
			responseRecorder := httptest.NewRecorder()

			adapter.forwardChatCompletionsRequest(responseRecorder, request)
			tc.validateResponse(assert, responseRecorder)
		})
	}
}

func TestVerifyOCSP(t *testing.T) {
	gpuPolicyFailure := "GPU attestation returned a GPU OCSP status that is not accepted by the client"
	driverPolicyFailure := "GPU attestation returned a driver OCSP status that is not accepted by the client"
	vbiosPolicyFailure := "GPU attestation returned a VBIOS OCSP status that is not accepted by the client"

	testCases := map[string]struct {
		ocspStatus     ocsp.StatusInfo
		acceptedStatus []ocspheader.AllowStatus
		expectedCode   int
		expectedBody   string
	}{
		"all good, accepted good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusOK,
		},
		"all good, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			expectedCode: http.StatusOK,
		},
		"unknown gpu, accept good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusUnknown, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusInternalServerError,
			expectedBody:   gpuPolicyFailure,
		},
		"unknown driver, accept good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusUnknown},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusInternalServerError,
			expectedBody:   driverPolicyFailure,
		},
		"unknown vbios, accept good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusUnknown, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusInternalServerError,
			expectedBody:   vbiosPolicyFailure,
		},
		"revoked gpu, accept good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusRevoked(time.Now()), VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusInternalServerError,
			expectedBody:   gpuPolicyFailure,
		},
		"revoked driver, accept good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusRevoked(time.Now())},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusInternalServerError,
			expectedBody:   driverPolicyFailure,
		},
		"revoked vbios, accept good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusRevoked(time.Now()), Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusInternalServerError,
			expectedBody:   vbiosPolicyFailure,
		},
		"unknown gpu, accept unknown": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusUnknown, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown},
			expectedCode:   http.StatusOK,
		},
		"unknown driver, accept unknown": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusUnknown},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown},
			expectedCode:   http.StatusOK,
		},
		"unknown vbios, accept unknown": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusUnknown, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown},
			expectedCode:   http.StatusOK,
		},
		"revoked gpu, accept revoked": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusRevoked(time.Now()), VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown, ocspheader.AllowStatusRevoked},
			expectedCode:   http.StatusOK,
		},
		"revoked driver, accept revoked": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusRevoked(time.Now())},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown, ocspheader.AllowStatusRevoked},
			expectedCode:   http.StatusOK,
		},
		"revoked vbios, accept revoked": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusRevoked(time.Now()), Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown, ocspheader.AllowStatusRevoked},
			expectedCode:   http.StatusOK,
		},
		"unknown gpu, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusUnknown, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			expectedCode: http.StatusInternalServerError,
			expectedBody: gpuPolicyFailure,
		},
		"unknown driver, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusUnknown},
			expectedCode: http.StatusInternalServerError,
			expectedBody: driverPolicyFailure,
		},
		"unknown vbios, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusUnknown, Driver: ocsp.StatusGood},
			expectedCode: http.StatusInternalServerError,
			expectedBody: vbiosPolicyFailure,
		},
		"revoked gpu, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusRevoked(time.Now()), VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			expectedCode: http.StatusInternalServerError,
			expectedBody: gpuPolicyFailure,
		},
		"revoked driver, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusRevoked(time.Now())},
			expectedCode: http.StatusInternalServerError,
			expectedBody: driverPolicyFailure,
		},
		"revoked vbios, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusRevoked(time.Now()), Driver: ocsp.StatusGood},
			expectedCode: http.StatusInternalServerError,
			expectedBody: vbiosPolicyFailure,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			secret := bytes.Repeat([]byte{0x01}, 32)
			secretID := "test"
			a := &Adapter{
				cipher: &stubCipher{
					secretMap: map[string][]byte{secretID: secret},
				},
				forwarder:     &stubForwarder{},
				workloadTasks: []string{constants.WorkloadTaskGenerate},
				mutators: openai.DefaultRequestMutators{
					CacheSaltInjector:       stubRequestMutator,
					CacheSaltValidator:      stubRequestMutator,
					SecureImageURLValidator: stubRequestMutator,
				},
				log:        slog.Default(),
				ocspStatus: []ocsp.StatusInfo{tc.ocspStatus},
			}
			handler := a.ServeMux()

			createRequest := func(method, path string) (*httptest.ResponseRecorder, *http.Request) {
				request := httptest.NewRequest(method, path, http.NoBody)

				if tc.acceptedStatus != nil {
					ocspHeader := ocspheader.NewHeader(tc.acceptedStatus, time.Time{})
					policyHeader, err := ocspHeader.Marshal()
					require.NoError(err)
					policyMACHeader, err := ocspHeader.MarshalMACHeader([32]byte(secret))
					require.NoError(err)

					request.Header.Set(constants.PrivatemodeNvidiaOCSPPolicyHeader, policyHeader)
					request.Header.Set(constants.PrivatemodeNvidiaOCSPPolicyMACHeader, policyMACHeader)
					request.Header.Set(constants.PrivatemodeSecretIDHeader, secretID)
				}

				return httptest.NewRecorder(), request
			}

			// OCSP verification middleware needs to be applied to all endpoints that interact with vLLM, i.e. the GPU
			for _, path := range []string{
				openai.ChatCompletionsEndpoint, openai.LegacyCompletionsEndpoint, openai.EmbeddingsEndpoint,
				openai.TranscriptionsEndpoint, openai.TranslationsEndpoint,
			} {
				t.Run(path, func(t *testing.T) {
					assert := assert.New(t)
					responseRecorder, request := createRequest(http.MethodPost, path)

					handler.ServeHTTP(responseRecorder, request)
					assert.Equal(tc.expectedCode, responseRecorder.Code)
					assert.Contains(responseRecorder.Body.String(), tc.expectedBody)
				})
			}

			// Models endpoint is used for heartbeats and doesn't need the GPU
			// OCSP middleware must not be applied to allow health checks on this endpoint
			t.Run(openai.ModelsEndpoint, func(t *testing.T) {
				assert := assert.New(t)
				responseRecorder, request := createRequest(http.MethodGet, openai.ModelsEndpoint)

				handler.ServeHTTP(responseRecorder, request)
				assert.Equal(http.StatusOK, responseRecorder.Code)
			})
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

func (f *stubForwarder) Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMutator, forwarder.HeaderMutator, ...forwarder.Opts) {
}

func stubRequestMutator(_ *http.Request) error { return nil }
