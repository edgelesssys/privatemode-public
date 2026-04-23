// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT
package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/anthropic"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/ocspheader"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/server/stub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAPIKey           string = "testkey"
	defaultClientVersion string = "v1.999.0" // greater than any test case
)

func TestChatCompletionsPromptEncryption(t *testing.T) {
	apiKey := testAPIKey
	testCases := map[string]struct {
		clientVersion    string
		proxyAPIKey      *string
		prompt           any
		requestCacheSalt string
		proxyCacheSalt   string
		expectStatusCode int
		expectedHeaders  map[string]string
		expectedBody     string
		requestMutator   func(*http.Request)
		isApp            bool
	}{
		"with privatemode-proxy API key": {
			proxyAPIKey:      &apiKey,
			prompt:           "Hello",
			expectStatusCode: http.StatusOK,
		},
		"without any API key": {
			expectStatusCode: http.StatusUnauthorized,
			prompt:           "Hello",
			expectedBody:     makeErrorMsg("no auth found: expected Authorization header with 'Bearer <auth>'"),
		},
		"with wrong API key": {
			expectStatusCode: http.StatusUnauthorized,
			prompt:           "Hello",
			expectedBody:     makeErrorMsg("invalid API key: invalid API key"),
			proxyAPIKey:      toPtr("wrongkey"),
		},
		"without privatemode-proxy API key but request contains Auth header": {
			expectStatusCode: http.StatusOK,
			prompt:           "Hello",
			requestMutator: func(req *http.Request) {
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", testAPIKey))
			},
		},
		"without cache salt key": {
			proxyAPIKey:      &apiKey,
			expectStatusCode: http.StatusOK,
			prompt:           "Hello",
		},
		"with valid request cache salt": {
			proxyAPIKey:      &apiKey,
			expectStatusCode: http.StatusOK,
			prompt:           "Hello",
			requestCacheSalt: "r1234567890123456789012345678912",
		},
		"with invalid request cache salt": {
			proxyAPIKey:      &apiKey,
			expectStatusCode: http.StatusInternalServerError, // TODO(dr75): fix http status codes in forwarders
			prompt:           "Hello",
			requestCacheSalt: "too short",
		},
		"with custom proxy cache salt": {
			proxyAPIKey:      &apiKey,
			expectStatusCode: http.StatusOK,
			prompt:           "Hello",
			proxyCacheSalt:   "p1234567890123456789012345678912",
		},
		"with custom proxy and request cache salt": {
			proxyAPIKey:      &apiKey,
			expectStatusCode: http.StatusOK,
			prompt:           "Hello",
			proxyCacheSalt:   "p1234567890123456789012345678912",
			requestCacheSalt: "r1234567890123456789012345678912",
		},
		"with app client": {
			proxyAPIKey:      &apiKey,
			expectStatusCode: http.StatusOK,
			prompt:           "Hello",
			isApp:            true,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			// Arrange server
			secret := secretmanager.Secret{
				ID:   "123",
				Data: bytes.Repeat([]byte{0x42}, 32),
			}
			if tc.clientVersion == "" {
				tc.clientVersion = defaultClientVersion
			}
			stubAuthBackendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				token := r.Header.Get("Authorization")
				if token == "" {
					forwarder.HTTPError(w, r, http.StatusUnauthorized, "no auth found: expected Authorization header with 'Bearer <auth>'")
					return
				}
				if token != fmt.Sprintf("Bearer %s", testAPIKey) {
					forwarder.HTTPError(w, r, http.StatusUnauthorized, "invalid API key: invalid API key")
					return
				}

				// test set headers
				appHeader := r.Header.Get(constants.PrivatemodeClientHeader)
				if tc.isApp {
					assert.Equal(constants.PrivatemodeClientApp, appHeader)
				} else {
					assert.Equal(constants.PrivatemodeClientProxy, appHeader)
				}
				assert.Equal(runtime.GOOS, r.Header.Get(constants.PrivatemodeOSHeader))
				assert.Equal(runtime.GOARCH, r.Header.Get(constants.PrivatemodeArchitectureHeader))

				// must override the version here as the proxy always sets 0.0.0
				r.Header.Set(constants.PrivatemodeVersionHeader, tc.clientVersion)

				stub.EchoHandler(secret.Map(), slog.Default()).ServeHTTP(w, r)
			}))
			defer stubAuthBackendServer.Close()

			sut := newTestServer(tc.proxyAPIKey, secret, stubAuthBackendServer.Listener.Addr().String(), tc.proxyCacheSalt, tc.isApp)

			// Act
			req := prepareChatRequest(t.Context(), require, &tc.prompt, nil, tc.requestCacheSalt)
			if tc.requestMutator != nil {
				tc.requestMutator(req)
			}
			resp := httptest.NewRecorder()
			sut.GetHandler().ServeHTTP(resp, req)

			if !assert.Equal(tc.expectStatusCode, resp.Code) {
				t.Logf("Body: %s", resp.Body.String())
			}
			if resp.Code == http.StatusOK {
				var res openai.ChatResponse
				require.NoError(json.NewDecoder(resp.Body).Decode(&res))
				require.Len(res.Choices, 1)
				assert.Equal(fmt.Sprintf("Echo: %v", tc.prompt), res.Choices[0].Message.Content)

				// cache salt should never be empty
				cacheSaltHeader := resp.Header().Get("Request-Cache-Salt")
				actualShardKey := resp.Header().Get("Request-Shard-Key")
				assert.NotEmpty(cacheSaltHeader)

				expectedCacheSalt := tc.proxyCacheSalt

				// cache salt is actually used; request cache salt takes precedence
				if tc.requestCacheSalt != "" {
					expectedCacheSalt = tc.requestCacheSalt
				}

				if expectedCacheSalt != "" {
					// explicit cache salt is used; requires shard key
					assert.Equal(expectedCacheSalt, cacheSaltHeader)

					hash := sha256.Sum256([]byte(expectedCacheSalt))
					expectedShardKey := hex.EncodeToString(hash[:8])
					assert.Equal(expectedShardKey, actualShardKey)
				} else {
					// random cache salt
					assert.NotEmpty(cacheSaltHeader)

					// no shard key if random salt
					assert.Empty(actualShardKey)
				}
			}
			if tc.expectedBody != "" {
				body, err := io.ReadAll(resp.Body)
				require.NoError(err)
				assert.Contains(string(body), tc.expectedBody)
			}

			for key, value := range tc.expectedHeaders {
				assert.Equal(value, resp.Header().Get(key))
			}
		})
	}
}

func TestInvalidSecretRetry(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	apiKey := testAPIKey
	secretInvalid := secretmanager.Secret{
		ID:   "123",
		Data: bytes.Repeat([]byte{0x42}, 32),
	}
	secretValid := secretmanager.Secret{
		ID:   "456",
		Data: bytes.Repeat([]byte{0x42}, 32),
	}

	// setup a server with secretValid and check that there are two requests
	// - the first fails due to the invalid secret
	// - the second succeeds with the valid secret
	var capturedHeaders []http.Header
	stubAuthBackendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = append(capturedHeaders, r.Header.Clone())
		token := r.Header.Get("Authorization")
		if token == "" {
			forwarder.HTTPError(w, r, http.StatusUnauthorized, "no auth found: expected Authorization header with 'Bearer <auth>'")
			return
		}
		if token != fmt.Sprintf("Bearer %s", testAPIKey) {
			forwarder.HTTPError(w, r, http.StatusUnauthorized, "invalid API key: invalid API key")
			return
		}

		stub.EchoHandler(secretValid.Map(), slog.Default()).ServeHTTP(w, r)
	}))
	defer stubAuthBackendServer.Close()

	sut := Server{
		apiKey:                       &apiKey,
		defaultCacheSalt:             "",
		sm:                           &stubSecretManager{secrets: []secretmanager.Secret{secretInvalid, secretValid}},
		forwarder:                    forwarder.New(http.DefaultClient, stubAuthBackendServer.Listener.Addr().String(), forwarder.SchemeHTTP, slog.Default()),
		log:                          slog.Default(),
		isApp:                        false,
		nvidiaOCSPAllowUnknown:       true,
		nvidiaOCSPRevokedGracePeriod: 24 * time.Hour,
	}

	prompt := "Hello"
	req := prepareChatRequest(t.Context(), require, &prompt, nil, "")
	resp := httptest.NewRecorder()
	sut.GetHandler().ServeHTTP(resp, req)

	require.Equal(http.StatusOK, resp.Code)
	require.Len(capturedHeaders, 2)

	var res openai.ChatResponse
	require.NoError(json.NewDecoder(resp.Body).Decode(&res))
	require.Len(res.Choices, 1)
	assert.Equal("Echo: Hello", res.Choices[0].Message.Content)

	// the test runs with automatically generated cache salt, so the header is set
	// but no shard key is generated as caching is disabled
	assert.NotEmpty(resp.Header().Get("Request-Cache-Salt"))
	assert.Empty(resp.Header().Get("Request-Shard-Key"))

	// each attempt gets fresh dynamic headers: the secret ID reflects the rotated secret and the
	// request ID differs per attempt
	assert.Equal(secretInvalid.ID, capturedHeaders[0].Get(constants.PrivatemodeSecretIDHeader))
	assert.Equal(secretValid.ID, capturedHeaders[1].Get(constants.PrivatemodeSecretIDHeader))
	assert.NotEqual(
		capturedHeaders[0].Get(constants.RequestIDHeader),
		capturedHeaders[1].Get(constants.RequestIDHeader),
	)
}

func TestTools(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	secret := secretmanager.Secret{
		ID:   "123",
		Data: bytes.Repeat([]byte{0x42}, 32),
	}

	stubAuthBackendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.EchoHandler(secret.Map(), slog.Default()).ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		stubAuthBackendServer.Close()
	})

	// Create the SUT once (or inside the loop if you prefer)
	apiKey := testAPIKey
	sut := newTestServer(&apiKey, secret, stubAuthBackendServer.Listener.Addr().String(), "", false)

	testFunc1 := `{"type":"function","function":"func1"}`
	testFunc1Echo := `{"type":"function","function":"func1","test":"echo"}`
	testFunc2 := `{"type":"function","function":"func2"}`
	testFunc2Echo := `{"type":"function","function":"func2","test":"echo"}`

	testCases := map[string]struct {
		prompt            *string
		tools             []string
		expectedContent   string
		expectedToolCalls []string
	}{
		// rare edge cases
		"no prompt, no functions array": {
			prompt:            nil,
			tools:             nil,
			expectedContent:   "Echo: nil",
			expectedToolCalls: nil,
		},
		"empty prompt, no functions array": {
			prompt:            strPtr(""),
			tools:             nil,
			expectedContent:   "Echo: ",
			expectedToolCalls: nil,
		},
		"with prompt, empty functions array": {
			prompt:            strPtr("Hello with 0 tools"),
			tools:             []string{},
			expectedContent:   "Echo: Hello with 0 tools",
			expectedToolCalls: nil,
		},
		// test returning the tool call back to the server (usually the message before tool response)
		"no prompt, one function": {
			prompt:            nil,
			tools:             []string{testFunc1},
			expectedContent:   "Echo: nil",
			expectedToolCalls: []string{testFunc1Echo},
		},
		// default case
		"with prompt, one function": {
			prompt:            strPtr("Hello with tools"),
			tools:             []string{testFunc1},
			expectedContent:   "Echo: Hello with tools",
			expectedToolCalls: []string{testFunc1Echo},
		},
		// test multiple tools
		"with prompt, two functions": {
			prompt:            strPtr("Hello with two tools"),
			tools:             []string{testFunc1, testFunc2},
			expectedContent:   "Echo: Hello with two tools",
			expectedToolCalls: []string{testFunc1Echo, testFunc2Echo},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			var tools []any
			for _, tool := range tc.tools {
				tools = append(tools, json.RawMessage(tool))
			}

			req := prepareChatRequest(t.Context(), require, tc.prompt, tools, "")
			resp := httptest.NewRecorder()
			sut.GetHandler().ServeHTTP(resp, req)
			require.Equal(http.StatusOK, resp.Code)

			// Decode response
			var res openai.ChatResponse
			require.NoError(json.NewDecoder(resp.Body).Decode(&res))
			require.Len(res.Choices, 1)

			// "Echo: <prompt>"
			msgContent := res.Choices[0].Message.Content
			assert.Equal(tc.expectedContent, msgContent)

			// Check the tool calls
			toolCalls := res.Choices[0].Message.ToolCalls
			if tc.expectedToolCalls == nil {
				// No tool calls
				require.Nil(toolCalls)
			} else {
				// One call per tool
				require.NotNil(toolCalls)
				require.Len(toolCalls, len(tc.expectedToolCalls))

				for i, call := range toolCalls {
					// Compare ignoring field ordering
					expected := (tc.expectedToolCalls)[i]
					assert.JSONEq(expected, call.(string))
				}
			}
		})
	}
}

func TestUnstructuredEncrypted(t *testing.T) {
	secret := secretmanager.Secret{
		ID:   "456",
		Data: bytes.Repeat([]byte{0x24}, 32),
	}

	formDataHandler := func(r *http.Request) (map[string]string, error) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			return nil, fmt.Errorf("parse form: %w", err)
		}

		fieldVal := r.FormValue("testField")

		file, _, err := r.FormFile("testContent")
		if err != nil {
			return nil, fmt.Errorf("reading file: %w", err)
		}
		defer file.Close()

		content, err := io.ReadAll(file)
		if err != nil {
			return nil, fmt.Errorf("reading file content: %w", err)
		}

		return map[string]string{
			"testField":   fieldVal,
			"testContent": string(content),
		}, nil
	}

	jsonHandler := func(r *http.Request) (map[string]string, error) {
		var body struct {
			TestField   string `json:"testField"`
			TestContent string `json:"testContent"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, fmt.Errorf("decode json: %w", err)
		}
		return map[string]string{
			"testField":   body.TestField,
			"testContent": body.TestContent,
		}, nil
	}

	testCases := map[string]struct {
		buildBody func(t *testing.T) (*bytes.Buffer, string)
		handler   func(r *http.Request) (map[string]string, error)
	}{
		"MultipartForm": {
			buildBody: func(t *testing.T) (*bytes.Buffer, string) {
				var buf bytes.Buffer
				writer := multipart.NewWriter(&buf)
				require.NoError(t, writer.WriteField("testField", "test field"))
				part, err := writer.CreateFormFile("testContent", "test.txt")
				require.NoError(t, err)
				_, err = part.Write([]byte("some content"))
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				return &buf, writer.FormDataContentType()
			},
			handler: formDataHandler,
		},
		"JSON": {
			buildBody: func(t *testing.T) (*bytes.Buffer, string) {
				payload := map[string]string{
					"testField":   "test field",
					"testContent": "some content",
				}
				var buf bytes.Buffer
				require.NoError(t, json.NewEncoder(&buf).Encode(payload))
				return &buf, "application/json"
			},
			handler: jsonHandler,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			stub := fullEncryptionStubServer(secret, tc.handler)
			t.Cleanup(func() { stub.Close() })

			sut := newTestServer(nil, secret, stub.Listener.Addr().String(), "", false)

			body, contentType := tc.buildBody(t)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/unstructured/general/v0/general", body)
			req.Header.Set("Content-Type", contentType)

			resp := httptest.NewRecorder()
			sut.GetHandler().ServeHTTP(resp, req)
			require.Equal(t, http.StatusOK, resp.Code)

			var jsonResp map[string]string
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&jsonResp))

			assert.Equal(t, "test field", jsonResp["testField"])
			assert.Equal(t, "some content", jsonResp["testContent"])
		})
	}
}

func TestGetOCSPHeaders(t *testing.T) {
	testCases := map[string]struct {
		OCSPAllowedStatuses []ocspheader.AllowStatus
		revocNbf            time.Time
		secret              [32]byte
		wantErr             bool
	}{
		"ok": {
			OCSPAllowedStatuses: []ocspheader.AllowStatus{
				ocspheader.AllowStatusGood, ocspheader.AllowStatusRevoked,
			},
			revocNbf: time.Now().Add(24 * time.Hour),
			secret:   [32]byte{0x42},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			policyHeader, macHeader, err := getOcspHeaders(tc.OCSPAllowedStatuses, tc.revocNbf, tc.secret)
			if tc.wantErr {
				require.Error(err)
			} else {
				require.NoError(err)
				assert.NotEmpty(policyHeader)
				assert.NotEmpty(macHeader)
			}
		})
	}
}

func TestSetDynamicHeaders(t *testing.T) {
	testCases := map[string]struct {
		secret  secretmanager.Secret
		wantErr bool
	}{
		"ok": {
			secret: secretmanager.Secret{
				ID:   "123",
				Data: bytes.Repeat([]byte{0x42}, 32),
			},
		},
		"too short secret": {
			secret: secretmanager.Secret{
				ID:   "123",
				Data: bytes.Repeat([]byte{0x42}, 8),
			},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			server := newTestServer(nil, tc.secret, "", "", false)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
			requestID := newRequestID()
			attempt := 1
			err := server.setDynamicHeaders(req, tc.secret, requestID, attempt)
			if tc.wantErr {
				require.Error(err)
			} else {
				require.NoError(err)
				assert.Equal(req.Header.Get(constants.PrivatemodeSecretIDHeader), tc.secret.ID)
				assert.NotEmpty(req.Header.Get(constants.PrivatemodeNvidiaOCSPPolicyHeader))
				assert.NotEmpty(req.Header.Get(constants.PrivatemodeNvidiaOCSPPolicyMACHeader))
				assert.Equal(req.Header.Get(constants.RequestIDHeader), fmt.Sprintf("%s_%d", requestID, attempt))
			}
		})
	}
}

func TestTargetModelHeader(t *testing.T) {
	// Random string to check verbatim inclusion in header
	randomModel := "Cu1pS7yT"

	testCases := map[string]struct {
		buildRequest  func(t *testing.T, require *require.Assertions) *http.Request
		expectedModel string
	}{
		"chat completions": {
			buildRequest: func(t *testing.T, require *require.Assertions) *http.Request {
				return prepareJSONRequest(t.Context(), require, openai.ChatCompletionsEndpoint, openai.ChatRequest{
					ChatRequestPlainData: openai.ChatRequestPlainData{
						Model: randomModel,
					},
					Messages: []openai.Message{
						{
							Role:    "user",
							Content: "Hello",
						},
					},
				})
			},
			expectedModel: randomModel,
		},
		"legacy (chat) completions": {
			buildRequest: func(t *testing.T, require *require.Assertions) *http.Request {
				return prepareJSONRequest(t.Context(), require, openai.LegacyCompletionsEndpoint, openai.ChatRequest{
					ChatRequestPlainData: openai.ChatRequestPlainData{
						Model: randomModel,
					},
					Messages: []openai.Message{
						{
							Role:    "user",
							Content: "Hello",
						},
					},
				})
			},
			expectedModel: randomModel,
		},
		"embeddings": {
			buildRequest: func(t *testing.T, require *require.Assertions) *http.Request {
				return prepareJSONRequest(t.Context(), require, openai.EmbeddingsEndpoint, openai.EmbeddingsRequest{
					EmbeddingsRequestPlainData: openai.EmbeddingsRequestPlainData{
						Model: randomModel,
					},
					Input: []string{"Hello"},
				})
			},
			expectedModel: randomModel,
		},
		"transcriptions": {
			buildRequest: func(t *testing.T, require *require.Assertions) *http.Request {
				return prepareMultiPartRequest(t.Context(), require, openai.TranscriptionsEndpoint, func(writer *multipart.Writer) error {
					if err := writer.WriteField("model", randomModel); err != nil {
						return err
					}

					part, err := writer.CreateFormFile("file", "audio.mp3")
					if err != nil {
						return err
					}
					fakeMP3 := []byte{0x49, 0x44, 0x33, 0x03, 0x00, 0x00} // Start of MP3 header
					if _, err := part.Write(fakeMP3); err != nil {
						return err
					}

					return nil
				})
			},
			expectedModel: randomModel,
		},
		"anthropic messages": {
			buildRequest: func(t *testing.T, require *require.Assertions) *http.Request {
				return prepareJSONRequest(t.Context(), require, anthropic.MessagesEndpoint, anthropic.MessagesRequest{
					MessagesRequestPlainData: anthropic.MessagesRequestPlainData{
						Model: randomModel,
					},
					Messages: []anthropic.Message{
						{
							Role:    "user",
							Content: "Hello",
						},
					},
				})
			},
			expectedModel: randomModel,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			secret := secretmanager.Secret{
				ID:   "123",
				Data: bytes.Repeat([]byte{0x42}, 32),
			}

			stubBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(tc.expectedModel, r.Header.Get(constants.PrivatemodeTargetModel))

				stub.EchoHandler(secret.Map(), slog.Default()).ServeHTTP(w, r)
			}))
			defer stubBackend.Close()

			apiKey := testAPIKey
			sut := newTestServer(&apiKey, secret, stubBackend.Listener.Addr().String(), "", false)

			req := tc.buildRequest(t, require)

			resp := httptest.NewRecorder()
			sut.GetHandler().ServeHTTP(resp, req)
			assert.Equal(http.StatusOK, resp.Code)
		})
	}
}

// newTestServer returns a stub server for testing.
func newTestServer(apiKey *string, secret secretmanager.Secret, backendAddr string, defaultCacheSalt string, isApp bool) *Server {
	return &Server{
		apiKey:                       apiKey,
		defaultCacheSalt:             defaultCacheSalt,
		sm:                           &stubSecretManager{secrets: []secretmanager.Secret{secret}},
		forwarder:                    forwarder.New(http.DefaultClient, backendAddr, forwarder.SchemeHTTP, slog.Default()),
		log:                          slog.Default(),
		isApp:                        isApp,
		nvidiaOCSPAllowUnknown:       true,
		nvidiaOCSPRevokedGracePeriod: time.Hour * 24,
	}
}

func fullEncryptionStubServer(secret secretmanager.Secret, handler func(r *http.Request) (map[string]string, error)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encrypt, decrypt := stub.GetEncryptionFunctions(secret.Map())
		log := slog.Default()

		// Use JSON request mutation if the request is a JSON request
		requestMutator := forwarder.WithRawRequestMutation(decrypt, log)
		if err := requestMutator(r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		data, err := handler(r)
		if err != nil {
			log.Error("handler error", "error", err)
			http.Error(w, fmt.Sprintf("handler error: %v", err), http.StatusBadRequest)
			return
		}

		responseJSON, err := json.Marshal(data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		mutatedJSON, err := forwarder.MutateJSONFields(responseJSON, encrypt, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(mutatedJSON); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
}

func prepareChatRequest(ctx context.Context, require *require.Assertions, content any, tools []any, cacheSalt string) *http.Request {
	return prepareJSONRequest(ctx, require, openai.ChatCompletionsEndpoint, openai.ChatRequest{
		ChatRequestPlainData: openai.ChatRequestPlainData{
			Model: "gpt-oss-120b",
		},
		Messages: []openai.Message{
			{
				Role:    "user",
				Content: content,
			},
		},
		Tools:     tools,
		CacheSalt: cacheSalt,
	})
}

type stubSecretManager struct {
	secrets    []secretmanager.Secret
	nextSecret int
}

func (s *stubSecretManager) LatestSecret(_ context.Context) (secretmanager.Secret, error) {
	if len(s.secrets) == 0 {
		return secretmanager.Secret{}, fmt.Errorf("no secrets available")
	}
	secret := s.secrets[s.nextSecret%len(s.secrets)]
	return secret, nil
}

func (s *stubSecretManager) ForceUpdate(_ context.Context) error {
	s.nextSecret = (s.nextSecret + 1) % len(s.secrets) // Reset to the next secret
	return nil
}

func (s *stubSecretManager) OfferAPIKey(context.Context, string) error {
	return nil
}

func toPtr(s string) *string {
	return &s
}

func makeErrorMsg(message string) string {
	msgBytes, err := json.Marshal(openai.APIErrorResponse{Error: openai.APIError{
		Message: message,
	}})
	if err != nil {
		panic(err)
	}
	return string(msgBytes)
}

func prepareJSONRequest(ctx context.Context, require *require.Assertions, path string, payload any) *http.Request {
	baseURL := "http://192.0.2.1:8080" // doesn't matter
	url := fmt.Sprintf("%s%s", baseURL, path)

	payloadBytes, err := json.Marshal(payload)
	require.NoError(err)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payloadBytes))
	require.NoError(err)

	req.Header.Set("Content-Type", "application/json")

	return req
}

func prepareMultiPartRequest(ctx context.Context, require *require.Assertions, path string, payloadFunc func(*multipart.Writer) error) *http.Request {
	baseURL := "http://192.0.2.1:8080" // doesn't matter
	url := fmt.Sprintf("%s%s", baseURL, path)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	require.NoError(payloadFunc(writer))
	require.NoError(writer.Close())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	require.NoError(err)

	req.Header.Set("Content-Type", writer.FormDataContentType())

	return req
}
