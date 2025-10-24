// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only
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

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/internal/gpl/ocspheader"
	"github.com/edgelesssys/continuum/internal/gpl/openai"
	"github.com/edgelesssys/continuum/internal/gpl/secretmanager"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/server/stub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAPIKey           string = "testkey"
	defaultClientVersion string = "v1.999.0" // greater than any test case
)

func TestPromptEncryption(t *testing.T) {
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
		"ACAO header is set for wails origin": {
			proxyAPIKey:      &apiKey,
			expectStatusCode: http.StatusOK,
			prompt:           "Hello",
			requestMutator: func(req *http.Request) {
				req.Header.Set("Origin", "wails://wails.localhost")
			},
			expectedHeaders: map[string]string{
				"Access-Control-Allow-Origin": "wails://wails.localhost",
			},
		},
		"ACAO header is set for app origin": {
			proxyAPIKey:      &apiKey,
			expectStatusCode: http.StatusOK,
			prompt:           "Hello",
			requestMutator: func(req *http.Request) {
				req.Header.Set("Origin", "app://-")
			},
			expectedHeaders: map[string]string{
				"Access-Control-Allow-Origin": "app://-",
			},
		},
		"ACAO header is unset for unknown origin": {
			proxyAPIKey:      &apiKey,
			expectStatusCode: http.StatusOK,
			prompt:           "Hello",
			requestMutator: func(req *http.Request) {
				req.Header.Set("Origin", "http://localhost")
			},
			expectedHeaders: map[string]string{
				"Access-Control-Allow-Origin": "",
			},
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
			stubAuthOpenAIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

				stub.OpenAIEchoHandler(secret.Map(), slog.Default()).ServeHTTP(w, r)
			}))
			defer stubAuthOpenAIServer.Close()

			sut := newTestServer(tc.proxyAPIKey, secret, stubAuthOpenAIServer.Listener.Addr().String(), tc.proxyCacheSalt, tc.isApp)

			// Act
			req := prepareRequest(t.Context(), require, &tc.prompt, nil, tc.requestCacheSalt)
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

func TestInvalidSecret(t *testing.T) {
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
	numRequests := 0
	stubAuthOpenAIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		numRequests++
		token := r.Header.Get("Authorization")
		if token == "" {
			forwarder.HTTPError(w, r, http.StatusUnauthorized, "no auth found: expected Authorization header with 'Bearer <auth>'")
			return
		}
		if token != fmt.Sprintf("Bearer %s", testAPIKey) {
			forwarder.HTTPError(w, r, http.StatusUnauthorized, "invalid API key: invalid API key")
			return
		}

		stub.OpenAIEchoHandler(secretValid.Map(), slog.Default()).ServeHTTP(w, r)
	}))
	defer stubAuthOpenAIServer.Close()

	sut := Server{
		apiKey:                       &apiKey,
		defaultCacheSalt:             "",
		sm:                           &stubSecretManager{secrets: []secretmanager.Secret{secretInvalid, secretValid}},
		forwarder:                    forwarder.New(http.DefaultClient, stubAuthOpenAIServer.Listener.Addr().String(), forwarder.SchemeHTTP, slog.Default()),
		log:                          slog.Default(),
		isApp:                        false,
		nvidiaOCSPAllowUnknown:       true,
		nvidiaOCSPRevokedGracePeriod: 24 * time.Hour,
	}

	prompt := "Hello"
	req := prepareRequest(t.Context(), require, &prompt, nil, "")
	resp := httptest.NewRecorder()
	sut.GetHandler().ServeHTTP(resp, req)

	require.Equal(http.StatusOK, resp.Code)
	assert.Equal(2, numRequests)

	var res openai.ChatResponse
	require.NoError(json.NewDecoder(resp.Body).Decode(&res))
	require.Len(res.Choices, 1)
	assert.Equal("Echo: Hello", res.Choices[0].Message.Content)

	// the test runs with automatically generated cache salt, so the header is set
	// but no shard key is generated as caching is disabled
	assert.NotEmpty(resp.Header().Get("Request-Cache-Salt"))
	assert.Empty(resp.Header().Get("Request-Shard-Key"))
}

func TestTools(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	secret := secretmanager.Secret{
		ID:   "123",
		Data: bytes.Repeat([]byte{0x42}, 32),
	}

	stubAuthOpenAIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.OpenAIEchoHandler(secret.Map(), slog.Default()).ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		stubAuthOpenAIServer.Close()
	})

	// Create the SUT once (or inside the loop if you prefer)
	apiKey := testAPIKey
	sut := newTestServer(&apiKey, secret, stubAuthOpenAIServer.Listener.Addr().String(), "", false)

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

			req := prepareRequest(t.Context(), require, tc.prompt, tools, "")
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

			req := httptest.NewRequest(http.MethodPost, "/unstructured/general/v0/general", body)
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

func TestShardKeyGeneration(t *testing.T) {
	server := &Server{log: slog.Default()}
	cacheSalt := "test-salt"

	// Test different token count ranges to verify k calculation
	testCases := map[string]struct {
		contentLength     int
		contentHashLength int
		contentKey        string
		expectError       bool
	}{
		"empty":                 {contentLength: 0, contentHashLength: 0},
		"1->0, block size 16*4": {contentLength: 1, contentHashLength: 0},
		// 1 block of 16 tokens
		"63->1, block size 16*4": {contentLength: 16*4 - 1, contentHashLength: 0},
		"64->1, block size 16*4": {contentLength: 16 * 4, contentHashLength: 1, contentKey: "Q"},
		// 2 blocks of 16 tokens
		"127->1, block size 16*4": {contentLength: 32*4 - 1, contentHashLength: 1, contentKey: "Q"},
		"128->2, block size 16*4": {contentLength: 32 * 4, contentHashLength: 2, contentKey: "QK"},
		"129->2, block size 16*4": {contentLength: 32*4 + 1, contentHashLength: 2, contentKey: "QK"},
		// 3 blocks of 16 tokens
		"191->2, block size 16*4":    {contentLength: 48*4 - 1, contentHashLength: 2, contentKey: "QK"},
		"192->3, block size 16*4":    {contentLength: 48 * 4, contentHashLength: 3, contentKey: "QKx"},
		"193->3, block size 16*4":    {contentLength: 48*4 + 1, contentHashLength: 3, contentKey: "QKx"},
		"4095->63, block size 16*4":  {contentLength: 1024*4 - 1, contentHashLength: 63, contentKey: "QKxToFQ1MRRq7Cv3lYFdwK6SCGf2xm2Lb85NGe7Z+Zy8goI7wAWd/zYccoVSVlj"},
		"4096->64, block size 128*4": {contentLength: 1024 * 4, contentHashLength: 64, contentKey: "QKxToFQ1MRRq7Cv3lYFdwK6SCGf2xm2Lb85NGe7Z+Zy8goI7wAWd/zYccoVSVljT"},
		"4097->64, block size 128*4": {contentLength: 1024*4 + 1, contentHashLength: 64, contentKey: "QKxToFQ1MRRq7Cv3lYFdwK6SCGf2xm2Lb85NGe7Z+Zy8goI7wAWd/zYccoVSVljT"},
		"4607->64, block size 128*4": {contentLength: (1024+128)*4 - 1, contentHashLength: 64, contentKey: "QKxToFQ1MRRq7Cv3lYFdwK6SCGf2xm2Lb85NGe7Z+Zy8goI7wAWd/zYccoVSVljT"},
		"4224->64, block size 128*4": {contentLength: (1024 + 128) * 4, contentHashLength: 65, contentKey: "QKxToFQ1MRRq7Cv3lYFdwK6SCGf2xm2Lb85NGe7Z+Zy8goI7wAWd/zYccoVSVljTI"},
		"100k-1, block size 128*4":   {contentLength: 100_096*4 - 1, contentHashLength: 64 + 773},
		"100k, block size 512*4":     {contentLength: 100_096 * 4, contentHashLength: 64 + 774},
		"100k+1, block size 512*4":   {contentLength: 100_096*4 + 1, contentHashLength: 64 + 774},
		"1M-1, block size 512*4":     {contentLength: 1_000_000*4 - 1, contentHashLength: 64 + 774 + 1757},
		"1M, block size 512*4":       {contentLength: 1_000_000 * 4, contentHashLength: 64 + 774 + 1757},
		"1M+0.75, block size 512*4":  {contentLength: 1_000_000*4 + 3, contentHashLength: 64 + 774 + 1757},
		"1M+1, error":                {contentLength: 1_000_000*4 + 4, contentHashLength: -1, expectError: true},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)
			content := string(bytes.Repeat([]byte("a"), tc.contentLength))

			shardKey, err := server.generateShardKey(cacheSalt, content)

			if tc.expectError {
				require.Error(err)
				return
			}

			require.NoError(err)

			// "saltHash-contentHash"
			shardKeyLength := constants.CacheSaltHashLength + tc.contentHashLength
			if tc.contentHashLength > 0 {
				shardKeyLength++ // '-'
			}
			assert.Len(shardKey, shardKeyLength)

			if len(tc.contentKey) > 0 {
				actualContentHash := shardKey[constants.CacheSaltHashLength:]
				assert.Equal("-"+tc.contentKey, actualContentHash)
			}
		})
	}
}

func BenchmarkShardKeyGeneration_1M(b *testing.B) {
	server := &Server{log: slog.Default()}
	cacheSalt := "test-salt"
	// 1M tokens -> contentLength: 1_000_000 * 4 (see unit test)
	content := string(bytes.Repeat([]byte("a"), 1_000_000*4))

	start := time.Now()
	for b.Loop() {
		if _, err := server.generateShardKey(cacheSalt, content); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
	avg := time.Since(start) / time.Duration(b.N)

	// 1 Mio tokens take about 2.5ms on a Mac M2 and ~20ms in CI; enforce <50ms per op.
	assert.Less(b, avg, 50*time.Millisecond, "shard key generation too slow")
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

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			err := server.setDynamicHeaders(req, tc.secret)
			if tc.wantErr {
				require.Error(err)
			} else {
				require.NoError(err)
				assert.Equal(req.Header.Get(constants.PrivatemodeSecretIDHeader), tc.secret.ID)
				assert.NotEmpty(req.Header.Get(constants.PrivatemodeNvidiaOCSPPolicyHeader))
				assert.NotEmpty(req.Header.Get(constants.PrivatemodeNvidiaOCSPPolicyMACHeader))
			}
		})
	}
}

// newTestServer returns a stub server for testing.
func newTestServer(apiKey *string, secret secretmanager.Secret, openAIServerAddr string, defaultCacheSalt string, isApp bool) *Server {
	return &Server{
		apiKey:                       apiKey,
		defaultCacheSalt:             defaultCacheSalt,
		sm:                           &stubSecretManager{secrets: []secretmanager.Secret{secret}},
		forwarder:                    forwarder.New(http.DefaultClient, openAIServerAddr, forwarder.SchemeHTTP, slog.Default()),
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
		requestMutator := forwarder.WithFullRequestMutation(decrypt, log)
		responseMutator := forwarder.WithFullJSONResponseMutation(encrypt, nil, false)

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

		w.Header().Set("Content-Type", "application/json")
		if _, err := io.Copy(w, responseMutator.Reader(bytes.NewReader(responseJSON))); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
}

func prepareRequest(ctx context.Context, require *require.Assertions, content any, tools []any, cacheSalt string) *http.Request {
	baseURL := "http://192.0.2.1:8080" // doesn't matter
	url := fmt.Sprintf("%s/v1/chat/completions", baseURL)

	payload := openai.ChatRequest{
		ChatRequestPlainData: openai.ChatRequestPlainData{
			Model: constants.DefaultTextgenModel,
		},
		Messages: []openai.Message{
			{
				Role:    "user",
				Content: content,
			},
		},
		Tools:     tools,
		CacheSalt: cacheSalt,
	}

	payloadBytes, err := json.Marshal(payload)
	require.NoError(err)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payloadBytes))
	require.NoError(err)

	req.Header.Set("Content-Type", "application/json")
	return req
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

func toPtr(s string) *string {
	return &s
}

func makeErrorMsg(message string) string {
	errObj := forwarder.APIError{
		Message: message,
	}
	msgBytes, err := json.Marshal(forwarder.ErrorMessage{Error: errObj})
	if err != nil {
		panic(err)
	}
	return string(msgBytes)
}
