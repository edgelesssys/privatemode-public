// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package openai provides type definitions for the OpenAI inference API.
// The marshaled value of the types defined here should never be returned to
// a client, or passed to the model backend.
// This is important to avoid dropping unknown fields sent by the client/model,
// or setting optional fields that are not expected by the client/model.
// For save modification of the requests/responses, use [sjson.SetBytes] instead.
//
// [sjson.SetBytes]: https://pkg.go.dev/github.com/tidwall/sjson#SetBytes
package openai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/mutators"
	"github.com/edgelesssys/continuum/internal/oss/usage"
)

const (
	// ChatCompletionsEndpoint is the endpoint for chat completions.
	ChatCompletionsEndpoint = "/v1/chat/completions"
	// LegacyCompletionsEndpoint is the legacy endpoint for chat completions.
	LegacyCompletionsEndpoint = "/v1/completions"
	// ModelsEndpoint is the endpoint to list the currently available models.
	ModelsEndpoint = "/v1/models"
	// EmbeddingsEndpoint is the endpoint for embeddings.
	EmbeddingsEndpoint = "/v1/embeddings"
	// TranscriptionsEndpoint is the endpoint for audio transcriptions.
	TranscriptionsEndpoint = "/v1/audio/transcriptions"
)

// StreamDone is the SSE data value that signals the end of a streaming
// response in the OpenAI-compatible API (e.g. "data: [DONE]").
var StreamDone = []byte("[DONE]")

// IsStreamDone reports whether value is the "[DONE]" marker that signals
// the end of an OpenAI-compatible streaming response.
func IsStreamDone(value []byte) bool {
	return bytes.Equal(value, StreamDone)
}

// RandomPromptCacheSalt generates a random salt for prompt caching and
// returns it as a base64-encoded string.
func RandomPromptCacheSalt() string {
	salt := make([]byte, 32) // 256-bit salt
	if _, err := rand.Read(salt); err != nil {
		// As per [rand.Read], it never returns an error.
		panic(fmt.Sprintf("generating random cache salt: %v", err))
	}
	return base64.StdEncoding.EncodeToString(salt)
}

// ChatRequest is the request structure for an OpenAI chat completion call.
//
// Don't send the marshalled type to clients/servers. Read package docs for more info.
type ChatRequest struct {
	Model               string         `json:"model"`
	MaxTokens           int            `json:"max_tokens,omitzero"` // deprecated in favor of max_completion_tokens
	MaxCompletionTokens int            `json:"max_completion_tokens,omitzero"`
	N                   int            `json:"n,omitzero"`
	Stream              bool           `json:"stream"`
	StreamOptions       *StreamOptions `json:"stream_options,omitempty"`
	Messages            []Message      `json:"messages"`
	Temperature         float32        `json:"temperature,omitzero"`
	Tools               []any          `json:"tools,omitempty"`
	CacheSalt           string         `json:"cache_salt,omitempty"`
}

// EmbeddingsRequest is the request structure for an OpenAI embeddings call.
type EmbeddingsRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	Dimensions     int      `json:"dimensions,omitzero"`
	EncodingFormat string   `json:"encoding_format,omitzero"`
	User           string   `json:"user,omitzero"`
}

// StreamOptions contains options for streaming completions. It is an extended version of the OpenAI StreamOptions type.
type StreamOptions struct {
	IncludeUsage         bool `json:"include_usage"`
	ContinuousUsageStats bool `json:"continuous_usage_stats"`
}

// ChatResponse is the response structure for an OpenAI chat completion call.
//
// Don't send the marshalled type to clients/servers. Read package docs for more info.
type ChatResponse struct {
	Choices []Choice `json:"choices"`
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int      `json:"created"`
	Model   string   `json:"model"`
	Usage   Usage    `json:"usage"`
}

// CompletionsResponse is the response structure for the legacy /v1/completions endpoint.
type CompletionsResponse struct {
	Choices []struct {
		Text  string `json:"text"`
		Index int    `json:"index"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

// ModelsResponse is the response structure for an OpenAI v1/models call.
type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// Model is the response structure for an OpenAI v1/models/{model} call.
// It is also used by [ModelsResponse].
type Model struct {
	ID      string   `json:"id,omitzero"`
	Object  string   `json:"object,omitzero"`
	Created int      `json:"created,omitzero"`
	OwnedBy string   `json:"owned_by,omitzero"`
	Tasks   []string `json:"tasks,omitzero"` // Custom parameter we add through the inference-proxy to differentiate workload capabilities
}

// Choice is a choice in an OpenAI chat completion call.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Message is a message in an OpenAI chat completion call.
type Message struct {
	Role      string `json:"role"`
	Content   any    `json:"content"`
	ToolCalls []any  `json:"tool_calls,omitempty"`
}

// Usage contains the token usage of an OpenAI chat completion call.
type Usage struct {
	PromptTokens        int                  `json:"prompt_tokens"`
	TotalTokens         int                  `json:"total_tokens"`
	CompletionTokens    int                  `json:"completion_tokens"`
	PromptTokensDetails *PromptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

// ToUsageStats converts an OpenAI [Usage] to a [usage.Stats].
func (u Usage) ToUsageStats() usage.Stats {
	promptTokens := int64(u.PromptTokens)
	var cachedPromptTokens int64
	if u.PromptTokensDetails != nil {
		cachedPromptTokens = int64(u.PromptTokensDetails.CachedTokens)
		promptTokens -= cachedPromptTokens
		if promptTokens < 0 {
			promptTokens = 0
		}
	}
	return usage.Stats{
		PromptTokens:       promptTokens,
		CachedPromptTokens: cachedPromptTokens,
		CompletionTokens:   int64(u.CompletionTokens),
	}
}

// PromptTokensDetails contains detailed information about prompt tokens.
type PromptTokensDetails struct {
	AudioTokens  int `json:"audio_tokens,omitzero"`
	CachedTokens int `json:"cached_tokens,omitzero"`
}

// TranscriptionUsageResponse contains all usage information provided by vLLM transcription responses.
type TranscriptionUsageResponse struct {
	Duration int        `json:"duration,omitzero"`
	Usage    AudioUsage `json:"usage,omitzero"`
}

// AudioUsage contains usage information of audio endpoints, e.g. /v1/audio/transcriptions.
type AudioUsage struct {
	Type             string `json:"type"`
	Seconds          int    `json:"seconds,omitzero"`
	PromptTokens     int    `json:"prompt_tokens,omitzero"`
	TotalTokens      int    `json:"total_tokens,omitzero"`
	CompletionTokens int    `json:"completion_tokens,omitzero"`
}

// APIErrorResponse represents the error response returned by the /v1/chat/completions endpoint.
//
// Occurrence:
//
//   - Non-streaming: Returned as the body with an HTTP error status.
//   - Streaming (pre-stream): Same as non-streaming, returned with content-type application/json.
//   - Streaming (mid-stream): Returned as the "data: {...}" of an SSE event. Not standardized.
type APIErrorResponse struct {
	Error APIError `json:"error"`
}

// APIError contains error details.
type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitzero"`
	// Code is a machine-readable identifier, but is encoded inconsistently. It is a string code
	// for OpenAI but an int (HTTP status code) in vLLM.
	Code json.RawMessage `json:"code,omitzero"`
}

// UnmarshalJSON implements custom JSON unmarshalling for TranscriptionUsageResponse to round
// Duration up to whole seconds.
func (a *TranscriptionUsageResponse) UnmarshalJSON(data []byte) error {
	var aux struct {
		// For requests with response_format=verbose_json, a top-level `duration` attribute is set
		// containing a float of the duration in seconds. vLLM encoded it as a string up to
		// v0.24.0, which we don't support anymore.
		Duration float64    `json:"duration,omitzero"`
		Usage    AudioUsage `json:"usage,omitzero"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	a.Duration = ceilSeconds(aux.Duration)
	a.Usage = aux.Usage
	return nil
}

// UnmarshalJSON implements custom JSON unmarshalling for AudioUsage to accept a fractional
// Seconds, which the OpenAI API specifies as a number.
func (a *AudioUsage) UnmarshalJSON(data []byte) error {
	var aux struct {
		Type             string  `json:"type"`
		Seconds          float64 `json:"seconds,omitzero"`
		PromptTokens     int     `json:"prompt_tokens,omitzero"`
		TotalTokens      int     `json:"total_tokens,omitzero"`
		CompletionTokens int     `json:"completion_tokens,omitzero"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	*a = AudioUsage{
		Type:             aux.Type,
		Seconds:          ceilSeconds(aux.Seconds),
		PromptTokens:     aux.PromptTokens,
		TotalTokens:      aux.TotalTokens,
		CompletionTokens: aux.CompletionTokens,
	}
	return nil
}

// ceilSeconds rounds seconds up to whole seconds, as we bill started seconds.
func ceilSeconds(seconds float64) int {
	return int(math.Ceil(seconds))
}

// CacheSaltGenerator returns a cache salt for the vLLM prompt cache.
type CacheSaltGenerator func() string

// DefaultRequestMutators is the default set of [forwarder.RequestMutator]s used by the vLLM adapter.
type DefaultRequestMutators struct {
	AudioStreamUsageReportingInjector forwarder.RequestMutator // AudioStreamUsageReportingInjector ensures vLLM includes usage stats in streaming audio responses.
	CacheSaltInjector                 forwarder.RequestMutator // CacheSaltInjector ensures a vLLM prompt cache salt is set.
	CacheSaltValidator                forwarder.RequestMutator // CacheSaltValidator validates the vLLM prompt cache set.
	MediaContentValidator             forwarder.RequestMutator // MediaContentValidator enforces the policy on media content blocks in the request.
	StreamUsageReportingInjector      forwarder.RequestMutator // StreamUsageReportingInjector ensures vLLM includes usage stats in streaming completion responses.
}

// GetDefaultRequestMutators returns the default set of [forwarder.RequestMutator]s used by the vLLM adapter.
func GetDefaultRequestMutators(cacheSaltGenerator CacheSaltGenerator, log *slog.Logger) DefaultRequestMutators {
	return DefaultRequestMutators{
		AudioStreamUsageReportingInjector: AudioStreamUsageReportingInjector(log),
		CacheSaltInjector:                 CacheSaltInjector(cacheSaltGenerator, log),
		CacheSaltValidator:                CacheSaltValidator(log),
		MediaContentValidator:             MediaContentValidator(log),
		StreamUsageReportingInjector:      StreamUsageReportingInjector(log),
	}
}

// CacheSaltInjector creates a [forwarder.RequestMutator] that injects a cache salt if it is not set.
func CacheSaltInjector(cacheSaltGenerator CacheSaltGenerator, log *slog.Logger) forwarder.RequestMutator {
	injectSalt := func(httpBody string) (mutatedRequest string, err error) {
		// Skip empty body, e.g., for OPTIONS requests
		if len(httpBody) == 0 {
			return httpBody, nil
		}
		currentSalt := gjson.Get(httpBody, "cache_salt").String()
		if currentSalt != "" {
			if len(currentSalt) < 32 {
				return "", fmt.Errorf("cache_salt must be at least 32 characters long")
			}
			return httpBody, nil
		}

		mutatedBody, err := sjson.Set(httpBody, "cache_salt", cacheSaltGenerator())
		if err != nil {
			return "", fmt.Errorf("injecting cache salt: %w", err)
		}
		return mutatedBody, nil
	}
	return forwarder.WithRequestMutation(injectSalt, log)
}

// CacheSaltValidator creates a [forwarder.RequestMutator] that ensures a non-empty cache salt.
func CacheSaltValidator(log *slog.Logger) forwarder.RequestMutator {
	validateSalt := func(httpBody string) (mutatedRequest string, err error) {
		// Skip empty body, e.g., for OPTIONS requests
		if len(httpBody) == 0 {
			return httpBody, nil
		}
		cacheSalt := gjson.Get(httpBody, "cache_salt").String()
		if cacheSalt == "" {
			return "", fmt.Errorf("missing field 'cache_salt'")
		}
		if len(cacheSalt) < 32 {
			return "", fmt.Errorf("cache_salt must be at least 32 characters long")
		}
		return httpBody, nil
	}

	return forwarder.WithRequestMutation(validateSalt, log)
}

// MediaContentValidator creates a [forwarder.RequestMutator] that enforces policy on media
// content blocks in the request. Image URLs must use data scheme via [validateImageURL]
// and audio via [validateAudioURL] and video via [validateVideoURL] content is not allowed.
func MediaContentValidator(log *slog.Logger) forwarder.RequestMutator {
	validate := func(httpBody string) (mutatedRequest string, err error) {
		// Skip empty body, e.g., for OPTIONS requests
		if len(httpBody) == 0 {
			return httpBody, nil
		}

		messages := gjson.Get(httpBody, "messages")
		if !messages.Exists() {
			// If we don't have the 'messages' field, we're in the legacy completions endpoint,
			// which doesn't support multi-media blocks at all.
			return httpBody, nil
		}

		// Images
		if err := validateStringQueryResults(httpBody, []string{
			// Images in message content:
			// #(type=="image_url") on the content block element is usually present, but optional for vLLM
			`messages.#.content.#.image_url.url|@flatten`,
			`messages.#.content.#.image_url|@flatten`,
		}, validateImageURL); err != nil {
			return "", fmt.Errorf("validating image URLs: %w", err)
		}

		// Audio
		if err := validateStringQueryResults(httpBody, []string{
			// Audio in message content:
			// #(type=="audio_url") on the content block element is usually present, but optional for vLLM
			`messages.#.content.#.audio_url.url|@flatten`,
			`messages.#.content.#.audio_url|@flatten`,
		}, validateAudioURL); err != nil {
			return "", fmt.Errorf("validating audio URLs: %w", err)
		}

		// Videos
		if err := validateStringQueryResults(httpBody, []string{
			// Video in message content:
			// #(type=="video_url") on the content block element is usually present, but optional for vLLM
			`messages.#.content.#.video_url.url|@flatten`,
			`messages.#.content.#.video_url|@flatten`,
		}, validateVideoURL); err != nil {
			return "", fmt.Errorf("validating video URLs: %w", err)
		}

		return httpBody, nil
	}

	return forwarder.WithRequestMutation(validate, log)
}

func validateStringQueryResults(document string, queries []string, validate func(string) error) error {
	for _, query := range queries {
		var retErr error
		gjson.Get(document, query).ForEach(func(_, res gjson.Result) bool {
			if res.Type == gjson.String {
				retErr = validate(res.String())
			}
			return retErr == nil
		})
		if retErr != nil {
			return retErr
		}
	}
	return nil
}

func validateImageURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parsing image URL: %w", err)
	}

	if !strings.EqualFold(parsedURL.Scheme, "data") {
		return mutators.ErrRemoteResourceURL
	}
	return nil
}

func validateAudioURL(rawURL string) error {
	return fmt.Errorf("audio URLs are not allowed: %q", rawURL)
}

func validateVideoURL(rawURL string) error {
	return fmt.Errorf("video URLs are not allowed: %q", rawURL)
}

// StreamUsageReportingInjector creates a [forwarder.RequestMutator] that ensures vLLM includes usage
// statistics in streaming completion responses. It sets stream_options.include_usage and
// stream_options.continuous_usage_stats to true.
func StreamUsageReportingInjector(log *slog.Logger) forwarder.RequestMutator {
	injectStreamOptions := func(httpBody string) (string, error) {
		log.DebugContext(context.Background(), "Enabling token tracking for request")

		if !gjson.Valid(httpBody) {
			return "", fmt.Errorf("request body is not valid json")
		}

		// stream_options is only relevant for streaming completions.
		if !gjson.Get(httpBody, "stream").Bool() {
			return httpBody, nil
		}

		// Middleware must not unmarshal the request body to a struct and then marshal it back to JSON,
		// because data loss may occur if the request body contains unknown fields.
		// Instead, use sjon to safely modify the request body.

		// Always include usage statistics in streaming completions.
		body, err := sjson.Set(httpBody, "stream_options.include_usage", true)
		if err != nil {
			return "", fmt.Errorf("setting stream_options.include_usage: %w", err)
		}
		// Always include continuous usage stats to allow early stopping of requests.
		body, err = sjson.Set(body, "stream_options.continuous_usage_stats", true)
		if err != nil {
			return "", fmt.Errorf("setting stream_options.continuous_usage_stats: %w", err)
		}
		return body, nil
	}
	return forwarder.WithRequestMutation(injectStreamOptions, log)
}

// AudioStreamUsageReportingInjector creates a [forwarder.RequestMutator] that ensures vLLM includes
// usage statistics in streaming audio transcription responses. It sets stream_include_usage and
// stream_continuous_usage_stats to true in the request form.
func AudioStreamUsageReportingInjector(log *slog.Logger) forwarder.RequestMutator {
	return func(r *http.Request) error {
		return forwarder.WithFormRequestMutation(func(form *multipart.Form, writer *multipart.Writer) (bool, error) {
			log.DebugContext(r.Context(), "Enabling audio token tracking for request")

			if vs := form.Value["stream"]; len(vs) == 0 || !strings.EqualFold(vs[0], "true") {
				log.DebugContext(r.Context(), "Non-streaming transcription request, no option modification needed")
				return false, nil
			}

			if err := writer.WriteField("stream_include_usage", "true"); err != nil {
				return false, fmt.Errorf("writing stream_include_usage field: %w", err)
			}
			if err := writer.WriteField("stream_continuous_usage_stats", "true"); err != nil {
				return false, fmt.Errorf("writing stream_continuous_usage_stats field: %w", err)
			}

			for key, values := range form.Value {
				if len(values) == 0 {
					continue
				}
				if key == "stream_include_usage" || key == "stream_continuous_usage_stats" {
					// We already set these fields to enabled, regardless of what the user provided
					continue
				}
				if err := writer.WriteField(key, values[0]); err != nil {
					return false, fmt.Errorf("writing form field %q: %w", key, err)
				}
			}
			for fileKey, fileHeaders := range form.File {
				if len(fileHeaders) == 0 {
					continue
				}
				formFile, err := fileHeaders[0].Open()
				if err != nil {
					return false, fmt.Errorf("opening form file %q: %w", fileKey, err)
				}
				fileWriter, err := writer.CreateFormFile(fileKey, fileKey)
				if err != nil {
					_ = formFile.Close()
					return false, fmt.Errorf("creating form file writer %q: %w", fileKey, err)
				}
				if _, err := io.Copy(fileWriter, formFile); err != nil {
					_ = formFile.Close()
					return false, fmt.Errorf("copying form file %q: %w", fileKey, err)
				}
				if err := formFile.Close(); err != nil {
					return false, fmt.Errorf("closing form file %q: %w", fileKey, err)
				}
			}

			return true, nil
		}, log)(r)
	}
}
