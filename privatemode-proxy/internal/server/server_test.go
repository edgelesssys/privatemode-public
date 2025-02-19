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
	"time"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/internal/gpl/openai"
	"github.com/edgelesssys/continuum/internal/gpl/openai/stub"
	"github.com/edgelesssys/continuum/internal/gpl/secretmanager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptEncryption(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	apiKey := "testkey"

	// Arrange stub server
	log := slog.Default()
	secret := secretmanager.Secret{
		ID:   "123",
		Data: bytes.Repeat([]byte{0x42}, 32),
	}
	stubAuthOpenAIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != fmt.Sprintf("Bearer %s", apiKey) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		stub.OpenAIEchoHandler(secret.Map(), log).ServeHTTP(w, r)
	}))
	defer stubAuthOpenAIServer.Close()

	testCases := map[string]struct {
		apiKey           *string
		expectStatusCode int
		requestMutator   func(*http.Request)
	}{
		"with privatemode-proxy API key": {
			apiKey:           &apiKey,
			expectStatusCode: http.StatusOK,
		},
		"without privatemode-proxy API key": {
			apiKey:           nil,
			expectStatusCode: http.StatusUnauthorized,
		},
		"without privatemode-proxy API key but request contains Auth header": {
			apiKey:           nil,
			expectStatusCode: http.StatusOK,
			requestMutator: func(req *http.Request) {
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
			},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// Arrange server
			sut := &Server{
				apiKey:    tc.apiKey,
				sm:        stubSecretManager{Secret: secret},
				forwarder: forwarder.New("tcp", stubAuthOpenAIServer.Listener.Addr().String(), log),
				log:       log,
			}

			// Act
			prompt := "Hello"
			req := prepareRequest(ctx, require, prompt)
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
				assert.Equal("Echo: Hello", res.Choices[0].Message.Content)
			}
		})
	}
}

func prepareRequest(ctx context.Context, require *require.Assertions, prompt string) *http.Request {
	baseURL := "http://192.0.2.1:8080" // doesn't matter
	url := fmt.Sprintf("%s/v1/chat/completions", baseURL)
	content := []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		{
			Role:    "user",
			Content: prompt,
		},
	}
	bt, err := json.Marshal(content)
	require.NoError(err)

	payload := openai.EncryptedChatRequest{
		Model:    constants.ServedModel,
		Messages: string(bt),
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

func (s stubSecretManager) LatestSecret(_ context.Context, _ time.Time) (secretmanager.Secret, error) {
	return s.Secret, nil
}
