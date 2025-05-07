// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// package openai provides type definitions for the OpenAI inference API.
// The marshaled value of the types defined here should never be returned to
// a client, or passed to the model backend.
// This is important to avoid dropping unknown fields sent by the client/model,
// or setting optional fields that are not expected by the client/model.
// For save modification of the requests/responses, use [sjson.SetBytes] instead.
//
// [sjson.SetBytes]: https://pkg.go.dev/github.com/tidwall/sjson#SetBytes
package openai

import "github.com/edgelesssys/continuum/internal/gpl/forwarder"

const (
	// ChatRequestMessagesField is the messages field in the request that is encrypted / decrypted.
	ChatRequestMessagesField = "messages"
	// ChatRequestToolsField is the tools field in the request that is encrypted / decrypted.
	ChatRequestToolsField = "tools"
	// ChatResponseEncryptionField is the field in the response that is encrypted / decrypted.
	ChatResponseEncryptionField = "choices"
	// ChatCompletionsEndpoint is the endpoint for chat completions.
	ChatCompletionsEndpoint = "/v1/chat/completions"
	// ModelsEndpoint is the endpoint to list the currently available models.
	ModelsEndpoint = "/v1/models"
	// EmbeddingsEndpoint is the endpoint for embeddings.
	EmbeddingsEndpoint = "/v1/embeddings"
)

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
}

// ChatRequest is the request structure for an OpenAI chat completion call.
//
// Don't send the marshalled type to clients/servers. Read package docs for more info.
type ChatRequest struct {
	ChatRequestPlainData
	Messages    []Message `json:"messages"`
	Temperature float32   `json:"temperature,omitzero"`
	Tools       []any     `json:"tools,omitempty"`
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
	Role      string  `json:"role"`
	Content   *string `json:"content"`
	ToolCalls []any   `json:"tool_calls,omitempty"`
}

// Usage contains the token usage of an OpenAI chat completion call.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}
