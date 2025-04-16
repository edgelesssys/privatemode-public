// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/internal/gpl/openai"
	"github.com/edgelesssys/continuum/internal/gpl/openai/stub"
	"github.com/edgelesssys/continuum/internal/gpl/secretmanager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testAPIKey string = "testkey"

func TestPromptEncryption(t *testing.T) {
	apiKey := testAPIKey
	testCases := map[string]struct {
		apiKey           *string
		expectStatusCode int
		expectedHeaders  map[string]string
		requestMutator   func(*http.Request)
	}{
		"with privatemode-proxy API key": {
			apiKey:           &apiKey,
			expectStatusCode: http.StatusOK,
		},
		"without privatemode-proxy API key": {
			expectStatusCode: http.StatusUnauthorized,
		},
		"without privatemode-proxy API key but request contains Auth header": {
			expectStatusCode: http.StatusOK,
			requestMutator: func(req *http.Request) {
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", testAPIKey))
			},
		},
		"ACAO header is set for wails origin": {
			apiKey:           &apiKey,
			expectStatusCode: http.StatusOK,
			requestMutator: func(req *http.Request) {
				req.Header.Set("Origin", "wails://wails.localhost")
			},
			expectedHeaders: map[string]string{
				"Access-Control-Allow-Origin": "wails://wails.localhost",
			},
		},
		"ACAO header is unset for unknown origin": {
			apiKey:           &apiKey,
			expectStatusCode: http.StatusOK,
			requestMutator: func(req *http.Request) {
				req.Header.Set("Origin", "http://localhost")
			},
			expectedHeaders: map[string]string{
				"Access-Control-Allow-Origin": "",
			},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			// Arrange server
			secret := secretmanager.Secret{
				ID:   "123",
				Data: bytes.Repeat([]byte{0x42}, 32),
			}
			stubAuthOpenAIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != fmt.Sprintf("Bearer %s", testAPIKey) {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
				stub.OpenAIEchoHandler(secret.Map(), slog.Default()).ServeHTTP(w, r)
			}))
			defer stubAuthOpenAIServer.Close()

			sut := newTestServer(tc.apiKey, secret, stubAuthOpenAIServer.Listener.Addr().String())

			// Act
			prompt := "Hello"
			req := prepareRequest(t.Context(), require, &prompt, nil)
			if tc.requestMutator != nil {
				tc.requestMutator(req)
			}
			resp := httptest.NewRecorder()
			sut.GetHandler().ServeHTTP(resp, req)

			// Assert
			assert := assert.New(t)
			assert.Equal(tc.expectStatusCode, resp.Code)
			if resp.Code == http.StatusOK {
				var res openai.ChatResponse
				require.NoError(json.NewDecoder(resp.Body).Decode(&res))
				require.Len(res.Choices, 1)
				assert.Equal("Echo: Hello", *res.Choices[0].Message.Content)
			}

			for key, value := range tc.expectedHeaders {
				assert.Equal(value, resp.Header().Get(key))
			}
		})
	}
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
	sut := newTestServer(&apiKey, secret, stubAuthOpenAIServer.Listener.Addr().String())

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

			req := prepareRequest(t.Context(), require, tc.prompt, tools)
			resp := httptest.NewRecorder()
			sut.GetHandler().ServeHTTP(resp, req)
			require.Equal(http.StatusOK, resp.Code)

			// Decode response
			var res openai.ChatResponse
			require.NoError(json.NewDecoder(resp.Body).Decode(&res))
			require.Len(res.Choices, 1)

			// "Echo: <prompt>"
			msgContent := res.Choices[0].Message.Content
			assert.Equal(tc.expectedContent, *msgContent)

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

// newTestServer returns a stub server for testing.
func newTestServer(apiKey *string, secret secretmanager.Secret, openAIServerAddr string) *Server {
	return &Server{
		apiKey:    apiKey,
		sm:        stubSecretManager{Secret: secret},
		forwarder: forwarder.New("tcp", openAIServerAddr, slog.Default()),
		log:       slog.Default(),
	}
}

func prepareRequest(ctx context.Context, require *require.Assertions, prompt *string, tools []any) *http.Request {
	baseURL := "http://192.0.2.1:8080" // doesn't matter
	url := fmt.Sprintf("%s/v1/chat/completions", baseURL)

	payload := openai.ChatRequest{
		ChatRequestPlainData: openai.ChatRequestPlainData{
			Model: constants.ServedModel,
		},
		Messages: []openai.Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Tools: tools,
	}

	payloadBytes, err := json.Marshal(payload)
	require.NoError(err)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payloadBytes))
	require.NoError(err)
	return req
}

type stubSecretManager struct {
	secretmanager.Secret
}

func (s stubSecretManager) LatestSecret(_ context.Context) (secretmanager.Secret, error) {
	return s.Secret, nil
}
