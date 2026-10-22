// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package openai

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/mutators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandomPromptCacheSalt(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		salt := RandomPromptCacheSalt()
		// vLLM v0.30 rejects '@', '/', '\\', NUL, and salts longer than 128 characters.
		require.False(t, strings.ContainsAny(salt, "@/\\\x00"))
		require.LessOrEqual(t, len(salt), 128)
		decoded, err := base64.URLEncoding.DecodeString(salt)
		require.NoError(t, err)
		require.Len(t, decoded, 32) // Preserve all 256 random bits.
		require.False(t, seen[salt], "salts must be fresh for each call")
		seen[salt] = true
	}
}

func TestRequestMutationValidationStatus(t *testing.T) {
	log := slog.Default()
	testCases := map[string]struct {
		mutator forwarder.RequestMutator
		body    string
	}{
		"cache salt injector rejects short salt": {
			mutator: CacheSaltInjector(RandomPromptCacheSalt, log),
			body:    `{"cache_salt":"short"}`,
		},
		"cache salt validator rejects missing salt": {
			mutator: CacheSaltValidator(log),
			body:    `{}`,
		},
		"cache salt validator rejects short salt": {
			mutator: CacheSaltValidator(log),
			body:    `{"cache_salt":"short"}`,
		},
		"stream options injector rejects invalid JSON": {
			mutator: StreamUsageReportingInjector(log),
			body:    `{"stream":true`,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tc.body))
			resp := httptest.NewRecorder()
			fwd := forwarder.New(nil, "upstream", forwarder.SchemeHTTP, forwarder.UpstreamModel, log)
			fwd.Forward(resp, req, tc.mutator, forwarder.PassthroughResponseMapper)
			assert.Equal(t, http.StatusBadRequest, resp.Code)
		})
	}
}

func TestLegacyCacheSaltHasher(t *testing.T) {
	salt := "Jp3c/Hn9Q2tWkx8aVb4ZrL6mYe0sTfUg1iOqP7dNwE5="
	digest := sha256.Sum256([]byte(salt))
	hashed := base64.URLEncoding.EncodeToString(digest[:])

	testCases := map[string]struct {
		clientVersion string
		body          string
		wantBody      string
	}{
		"client before v1.58 gets the salt hashed": {
			clientVersion: "v1.57.2",
			body:          `{"model":"model","cache_salt":"` + salt + `"}`,
			wantBody:      `{"model":"model","cache_salt":"` + hashed + `"}`,
		},
		"client v1.58 keeps its salt": {
			clientVersion: "v1.58.0",
			body:          `{"model":"model","cache_salt":"` + salt + `"}`,
			wantBody:      `{"model":"model","cache_salt":"` + salt + `"}`,
		},
		"body without salt is unchanged": {
			clientVersion: "v1.57.2",
			body:          `{"model":"model"}`,
			wantBody:      `{"model":"model"}`,
		},
		"empty body is unchanged": {
			clientVersion: "v1.57.2",
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tc.body))
			req.Header.Set(constants.PrivatemodeVersionHeader, tc.clientVersion)

			require.NoError(LegacyCacheSaltHasher(slog.Default())(req))

			after, err := io.ReadAll(req.Body)
			require.NoError(err)
			assert.Equal(t, tc.wantBody, string(after))
		})
	}
}

func TestMediaContentValidator(t *testing.T) {
	newRequest := func(content []map[string]any) *http.Request {
		t.Helper()
		data, err := json.Marshal(ChatRequest{
			Messages: []Message{
				{Role: "user", Content: content},
			},
		})
		if err != nil {
			panic(err)
		}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
			"https://foo.bar"+constants.OpenAIChatCompletionsEndpoint, bytes.NewReader(data))
		if err != nil {
			panic(err)
		}
		return req
	}

	testCases := map[string]struct {
		request     *http.Request
		validateErr func(error) bool
	}{
		"image/https url": {
			request: newRequest([]map[string]any{
				{
					"type":      "image_url",
					"image_url": map[string]string{"url": "https://example.com/image.jpg"},
				},
			}),
			validateErr: func(err error) bool {
				return errors.Is(err, mutators.ErrRemoteResourceURL)
			},
		},
		"image/http url": {
			request: newRequest([]map[string]any{
				{
					"type":      "image_url",
					"image_url": map[string]string{"url": "http://example.com/image.jpg"},
				},
			}),
			validateErr: func(err error) bool {
				return errors.Is(err, mutators.ErrRemoteResourceURL)
			},
		},
		"image/file url": {
			request: newRequest([]map[string]any{
				{
					"type":      "image_url",
					"image_url": map[string]string{"url": "file:///var/lib/image.png"},
				},
			}),
			validateErr: func(err error) bool {
				return errors.Is(err, mutators.ErrRemoteResourceURL)
			},
		},
		"image/data url": {
			request: newRequest([]map[string]any{
				{
					"type":      "image_url",
					"image_url": map[string]string{"url": "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAAYEBQYFBAYGBQYHBwYIChAKCgkJChQODwwQFxQYGBcUFhYaHSUfGhsjHBYWICwgIyYnKSopGR8tMC0oMCUoKSj/2wBDAQcHBwoIChMKChMoGhYa"},
				},
			}),
			validateErr: func(err error) bool { return err == nil },
		},
		"image/http url as second content block": {
			request: newRequest([]map[string]any{
				{
					"type":      "image_url",
					"image_url": map[string]string{"url": "https://example.com/image.jpg"},
				},
				{
					"type":      "image_url",
					"image_url": map[string]string{"url": "http://example.com/image.jpg"},
				},
			}),
			validateErr: func(err error) bool {
				return errors.Is(err, mutators.ErrRemoteResourceURL)
			},
		},
		"image/http url without type": {
			request: newRequest([]map[string]any{
				{
					"image_url": map[string]string{"url": "http://example.com/image.jpg"},
				},
			}),
			validateErr: func(err error) bool {
				return errors.Is(err, mutators.ErrRemoteResourceURL)
			},
		},
		"image/http url without type in concise schema": {
			request: newRequest([]map[string]any{
				{
					"image_url": "http://example.com/image.jpg",
				},
			}),
			validateErr: func(err error) bool {
				return errors.Is(err, mutators.ErrRemoteResourceURL)
			},
		},
		"video": {
			request: newRequest([]map[string]any{
				{
					"type":      "video_url",
					"video_url": map[string]string{"url": "https://example.com/video.mp4"},
				},
			}),
			validateErr: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), "not allowed")
			},
		},
		"video without type in concise schema": {
			request: newRequest([]map[string]any{
				{
					"video_url": "https://example.com/video.mp4",
				},
			}),
			validateErr: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), "not allowed")
			},
		},
		"audio": {
			request: newRequest([]map[string]any{
				{
					"type":      "audio_url",
					"audio_url": map[string]string{"url": "https://example.com/audio.mp3"},
				},
			}),
			validateErr: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), "not allowed")
			},
		},
		"audio without type in concise schema": {
			request: newRequest([]map[string]any{
				{
					"type":      "audio_url",
					"audio_url": "https://example.com/audio.mp3",
				},
			}),
			validateErr: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), "not allowed")
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			mutator := MediaContentValidator(logger)

			err := mutator(tc.request)
			require.True(tc.validateErr(err))
			if err != nil {
				require.Equal(http.StatusBadRequest, forwarder.StatusFromError(err))
			} else {
				require.Equal(http.StatusOK, forwarder.StatusFromError(err))
			}
		})
	}
}

func TestStreamUsageReportingInjector(t *testing.T) {
	testCases := map[string]struct {
		request           ChatRequest
		wantStreamOptions bool
	}{
		"set stream_options for streaming req": {
			request: ChatRequest{
				Messages:  []Message{{Role: "user", Content: "Hello!"}},
				Model:     "testmodel",
				MaxTokens: 1,
				N:         1,
				Stream:    true,
			},
			wantStreamOptions: true,
		},
		"no stream_options for non-streaming req": {
			request: ChatRequest{
				Messages:  []Message{{Role: "user", Content: "Hello!"}},
				Model:     "testmodel",
				MaxTokens: 1,
				N:         1,
			},
			wantStreamOptions: false,
		},
		"set stream_options even if continuous_usage_stats is set": {
			request: ChatRequest{
				Messages:  []Message{{Role: "user", Content: "Hello!"}},
				Model:     "testmodel",
				MaxTokens: 1,
				N:         1,
				Stream:    true,
				StreamOptions: &StreamOptions{
					ContinuousUsageStats: true,
				},
			},
			wantStreamOptions: true,
		},
		"set stream_options even if include_usage is set": {
			request: ChatRequest{
				Messages:  []Message{{Role: "user", Content: "Hello!"}},
				Model:     "testmodel",
				MaxTokens: 1,
				N:         1,
				Stream:    true,
				StreamOptions: &StreamOptions{
					IncludeUsage: true,
				},
			},
			wantStreamOptions: true,
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	url := "http://192.0.2.1" + constants.OpenAIChatCompletionsEndpoint

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			data, err := json.Marshal(tc.request)
			require.NoError(err)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(data))
			require.NoError(err)

			mutator := StreamUsageReportingInjector(logger)
			err = mutator(req)
			require.NoError(err)

			after, err := io.ReadAll(req.Body)
			require.NoError(err)

			var result ChatRequest
			require.NoError(json.Unmarshal(after, &result))

			assert.Equal(tc.request.Stream, result.Stream)
			if tc.wantStreamOptions {
				require.NotNil(result.StreamOptions)
				assert.True(result.StreamOptions.IncludeUsage)
				assert.True(result.StreamOptions.ContinuousUsageStats)
			} else if result.StreamOptions != nil {
				assert.False(result.StreamOptions.IncludeUsage)
				assert.False(result.StreamOptions.ContinuousUsageStats)
			}
		})
	}
}

func TestStreamUsageReportingInjectorExtraFields(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// The original request body, including a custom field not present in the ChatRequest struct.
	body := `{
		"stream":true, "messages":[{"role":"user","content":"Hello!"}], "model":"model", "max_tokens":1, "n":1, "temperature":0.5, "custom_field": "foo"
	}`
	// The expected modified request body, it should still include the unknown custom field.
	bodyExpected := `{
		"stream":true, "messages":[{"role":"user","content":"Hello!"}], "model":"model", "max_tokens":1, "n":1, "temperature":0.5, "custom_field": "foo",
		"stream_options": {
			"include_usage": true,
			"continuous_usage_stats": true
		}
	}`

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://192.0.2.1"+constants.OpenAIChatCompletionsEndpoint, bytes.NewReader([]byte(body)))
	require.NoError(err)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mutator := StreamUsageReportingInjector(logger)
	err = mutator(req)
	require.NoError(err)

	after, err := io.ReadAll(req.Body)
	require.NoError(err)

	var result ChatRequest
	require.NoError(json.Unmarshal(after, &result))
	assert.True(result.StreamOptions.IncludeUsage)
	assert.True(result.StreamOptions.ContinuousUsageStats)
	assert.JSONEq(bodyExpected, string(after))
}

func TestAudioStreamUsageReportingInjector(t *testing.T) {
	testCases := map[string]struct {
		createRequest     func(require *require.Assertions, writer *multipart.Writer)
		wantStreamOptions bool
	}{
		"non streaming request": {
			createRequest: func(require *require.Assertions, writer *multipart.Writer) {
				require.NoError(writer.WriteField("model", "testmodel"))
			},
			wantStreamOptions: false,
		},
		"streaming explicitly disabled": {
			createRequest: func(require *require.Assertions, writer *multipart.Writer) {
				require.NoError(writer.WriteField("model", "testmodel"))
				require.NoError(writer.WriteField("stream", "false"))
			},
			wantStreamOptions: false,
		},
		"streaming request": {
			createRequest: func(require *require.Assertions, writer *multipart.Writer) {
				require.NoError(writer.WriteField("model", "testmodel"))
				require.NoError(writer.WriteField("stream", "true"))
			},
			wantStreamOptions: true,
		},
		"streaming request with existing stream options": {
			createRequest: func(require *require.Assertions, writer *multipart.Writer) {
				require.NoError(writer.WriteField("model", "testmodel"))
				require.NoError(writer.WriteField("stream", "true"))
				require.NoError(writer.WriteField("stream_include_usage", "true"))
				require.NoError(writer.WriteField("stream_continuous_usage_stats", "true"))
			},
			wantStreamOptions: true,
		},
		"streaming request with disabled stream options": {
			createRequest: func(require *require.Assertions, writer *multipart.Writer) {
				require.NoError(writer.WriteField("model", "testmodel"))
				require.NoError(writer.WriteField("stream", "true"))
				require.NoError(writer.WriteField("stream_include_usage", "false"))
				require.NoError(writer.WriteField("stream_continuous_usage_stats", "false"))
			},
			wantStreamOptions: true,
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			tc.createRequest(require, writer)
			require.NoError(writer.Close())
			contentType := writer.FormDataContentType()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
				"http://192.0.2.1"+constants.OpenAITranscriptionsEndpoint, body)
			require.NoError(err)
			req.Header.Set("Content-Type", contentType)

			mutator := AudioStreamUsageReportingInjector(logger)
			err = mutator(req)
			require.NoError(err)

			bodyAfter, err := io.ReadAll(req.Body)
			require.NoError(err)

			// Create a new request so we can parse the multipart form again
			reqAfter, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
				"http://192.0.2.1"+constants.OpenAITranscriptionsEndpoint, bytes.NewReader(bodyAfter))
			require.NoError(err)
			reqAfter.Header.Set("Content-Type", req.Header.Get("Content-Type"))
			require.NoError(reqAfter.ParseMultipartForm(constants.MaxFileSizeBytes))

			if tc.wantStreamOptions {
				assert.Equal("true", reqAfter.FormValue("stream"))
				assert.Equal("true", reqAfter.FormValue("stream_include_usage"))
				assert.Equal("true", reqAfter.FormValue("stream_continuous_usage_stats"))
			} else {
				assert.NotEqual("true", reqAfter.FormValue("stream"))
			}
		})
	}
}
