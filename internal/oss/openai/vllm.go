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
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime/multipart"
	"net/url"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/usage"
)

const (
	// ChatRequestMessagesField is the messages field in the request that is encrypted / decrypted.
	ChatRequestMessagesField = "messages"
	// ChatRequestToolsField is the tools field in the request that is encrypted / decrypted.
	ChatRequestToolsField = "tools"
	// ChatResponseEncryptionField is the field in the response that is encrypted / decrypted.
	ChatResponseEncryptionField = "choices"
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

// PlainCompletionsRequestFields is a field selector for all fields in an OpenAI chat completions request that are not encrypted.
var PlainCompletionsRequestFields = forwarder.FieldSelector{
	{"model"},
	{"stream_options"},
	{"max_tokens"},
	{"max_completion_tokens"},
	{"n"},
	{"stream"},
}

// PlainCompletionsResponseFields is a field selector for all fields in an OpenAI chat completions response that are not encrypted.
var PlainCompletionsResponseFields = forwarder.FieldSelector{
	{"id"},
	{"usage"},
}

// PlainEmbeddingsRequestFields is a field selector for all fields in an OpenAI embeddings request that are not encrypted.
var PlainEmbeddingsRequestFields = forwarder.FieldSelector{
	{"model"},
}

// PlainEmbeddingsResponseFields is a field selector for all fields in an OpenAI embeddings response that are not encrypted.
var PlainEmbeddingsResponseFields = forwarder.FieldSelector{
	{"id"},
	{"usage"},
}

// PlainTranscriptionRequestFields are the plain form fields for OpenAI audio transcriptions.
var PlainTranscriptionRequestFields = forwarder.FieldSelector{
	{"model"},
	{"stream"},
	{"stream_include_usage"},
	{"stream_continuous_usage_stats"},
}

// PlainTranscriptionResponseFields is a field selector for all fields in an OpenAI transcription response that are not encrypted.
var PlainTranscriptionResponseFields = forwarder.FieldSelector{
	{"duration"},
	{"usage"},
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

// EncryptedChatRequest is the request structure for an OpenAI chat completion call,
// with encrypted fields.
// Fields that should not be encrypted need to be added to [PlainCompletionsRequestFields].
// See [ChatRequest] for the unencrypted request structure.
//
// Don't send the marshalled type to clients/servers. Read package docs for more info.
type EncryptedChatRequest struct {
	ChatRequestPlainData
	Messages    string  `json:"messages"` // The whole messages array from Request as an encrypted blob.
	Temperature string  `json:"temperature,omitzero"`
	Tools       *string `json:"tools,omitempty"` // The whole tools array from Request as an encrypted blob.
	CacheSalt   string  `json:"cache_salt,omitempty"`
}

// ChatRequest is the request structure for an OpenAI chat completion call.
//
// Don't send the marshalled type to clients/servers. Read package docs for more info.
type ChatRequest struct {
	ChatRequestPlainData
	Messages    []Message `json:"messages"`
	Temperature float32   `json:"temperature,omitzero"`
	Tools       []any     `json:"tools,omitempty"`
	CacheSalt   string    `json:"cache_salt,omitempty"`
}

// ChatRequestPlainData contains fields that are not encrypted for [ChatRequest] and [EncryptedChatRequest].
type ChatRequestPlainData struct {
	Model               string         `json:"model"`
	MaxTokens           int            `json:"max_tokens,omitzero"` // deprecated in favor of max_completion_tokens
	MaxCompletionTokens int            `json:"max_completion_tokens,omitzero"`
	N                   int            `json:"n,omitzero"`
	Stream              bool           `json:"stream"`
	StreamOptions       *StreamOptions `json:"stream_options,omitempty"`
}

// EncryptedEmbeddingsRequest is the request structure for an OpenAI embeddings call.
// Fields we currently don't use are omitted.
type EncryptedEmbeddingsRequest struct {
	EmbeddingsRequestPlainData
	Input          string `json:"input"`
	Dimensions     string `json:"dimensions,omitzero"`
	EncodingFormat string `json:"encoding_format,omitzero"`
	User           string `json:"user,omitzero"`
}

// EmbeddingsRequest is the request structure for an OpenAI embeddings call.
type EmbeddingsRequest struct {
	EmbeddingsRequestPlainData
	Input          []string `json:"input"`
	Dimensions     int      `json:"dimensions,omitzero"`
	EncodingFormat string   `json:"encoding_format,omitzero"`
	User           string   `json:"user,omitzero"`
}

// EmbeddingsRequestPlainData contains fields that are not encrypted for [EncryptedEmbeddingsRequest].
type EmbeddingsRequestPlainData struct {
	Model string `json:"model"`
}

// EncryptedEmbeddingsResponse is the response structure for an OpenAI embeddings call.
type EncryptedEmbeddingsResponse struct {
	Data   string `json:"data,omitzero"`
	Object string `json:"object,omitzero"`
	Model  string `json:"model,omitzero"`
	Usage  Usage  `json:"usage,omitzero"`
}

// StreamOptions contains options for streaming completions. It is an extended version of the OpenAI StreamOptions type.
type StreamOptions struct {
	IncludeUsage         bool `json:"include_usage"`
	ContinuousUsageStats bool `json:"continuous_usage_stats"`
}

// EncryptedChatResponse is the response structure for an OpenAI chat completion call,
// with an encrypted fields.
// Fields that should not be encrypted need to be added to [PlainCompletionsResponseFields].
// See [ChatResponse] for the unencrypted response structure.
//
// Don't send the marshalled type to clients/servers. Read package docs for more info.
type EncryptedChatResponse struct {
	// Choices is the whole choices array from Response as an encrypted blob.
	// The field is marked as omitzero to avoid adding empty strings in unit tests.
	Choices string `json:"choices,omitzero"`
	ID      string `json:"id,omitzero"`
	Object  string `json:"object,omitzero"`
	Created string `json:"created,omitzero"`
	Model   string `json:"model,omitzero"`
	Usage   Usage  `json:"usage,omitzero"`
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

// ToUsageStats converts an OpenAI [AudioUsage] to a [usage.Stats].
func (a AudioUsage) ToUsageStats() usage.Stats {
	return usage.Stats{
		AudioSeconds: int64(a.Seconds),
	}
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

// MarshalJSON implements custom JSON marshalling for AudioUsage to serialize Duration as a string.
func (a *TranscriptionUsageResponse) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Duration string     `json:"duration,omitzero"`
		Usage    AudioUsage `json:"usage,omitzero"`
	}{
		Duration: strconv.FormatFloat(float64(a.Duration), 'f', -1, 64),
		Usage:    a.Usage,
	})
}

// UnmarshalJSON implements custom JSON unmarshalling for AudioUsage to parse Duration from a string.
func (a *TranscriptionUsageResponse) UnmarshalJSON(data []byte) error {
	var aux struct {
		Duration string     `json:"duration,omitzero"`
		Usage    AudioUsage `json:"usage,omitzero"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	a.Usage = aux.Usage
	if aux.Duration != "" {
		duration, err := strconv.ParseFloat(aux.Duration, 64)
		if err != nil {
			return fmt.Errorf("parsing duration: %w", err)
		}
		a.Duration = int(math.Ceil(duration))
	}
	return nil
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
	return forwarder.WithRawRequestMutation(injectSalt, log)
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

	return forwarder.WithRawRequestMutation(validateSalt, log)
}

// MediaContentValidator creates a [forwarder.RequestMutator] that enforces policy on media
// content blocks in the request. Image URLs must use https or data schemes via [validateImageURL]
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

	return forwarder.WithRawRequestMutation(validate, log)
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

	if !strings.EqualFold(parsedURL.Scheme, "https") && !strings.EqualFold(parsedURL.Scheme, "data") {
		return fmt.Errorf("non-HTTPS and non-data image URL %q is insecure", parsedURL.String())
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
		log.Debug("Enabling token tracking for request")

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
	return forwarder.WithRawRequestMutation(injectStreamOptions, log)
}

// AudioStreamUsageReportingInjector creates a [forwarder.RequestMutator] that ensures vLLM includes
// usage statistics in streaming audio transcription responses. It sets stream_include_usage and
// stream_continuous_usage_stats to true in the request form.
func AudioStreamUsageReportingInjector(log *slog.Logger) forwarder.RequestMutator {
	return forwarder.WithRawFormRequestMutation(func(form *multipart.Form, writer *multipart.Writer) (bool, error) {
		log.Debug("Enabling audio token tracking for request")

		if vs := form.Value["stream"]; len(vs) == 0 || !strings.EqualFold(vs[0], "true") {
			log.Debug("Non-streaming transcription request, no option modification needed")
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
	}, log)
}
