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

const (
	// ChatRequestEncryptionField is the field in the request that is encrypted / decrypted.
	ChatRequestEncryptionField = "messages"
	// ChatResponseEncryptionField is the field in the response that is encrypted / decrypted.
	ChatResponseEncryptionField = "choices"
	// ChatCompletionsEndpoint is the endpoint for chat completions.
	ChatCompletionsEndpoint = "/v1/chat/completions"
	// ModelsEndpoint is the endpoint to list the currently available models.
	ModelsEndpoint = "/v1/models"
)

// EncryptedChatRequest is the request structure for an OpenAI chat completion call,
// with an encrypted "messages" field.
//
// Don't send the marshalled type to clients/servers. Read package docs for more info.
type EncryptedChatRequest struct {
	Messages      string         `json:"messages"` // The whole messages array from Request as an encrypted blob.
	Model         string         `json:"model"`
	Temperature   float32        `json:"temperature"`
	N             int            `json:"n"`
	MaxTokens     int            `json:"max_tokens"`
	Stream        bool           `json:"stream"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

// ChatRequest is the request structure for an OpenAI chat completion call.
//
// Don't send the marshalled type to clients/servers. Read package docs for more info.
type ChatRequest struct {
	Messages      []message      `json:"messages"`
	Model         string         `json:"model"`
	Temperature   float32        `json:"temperature"`
	N             int            `json:"n"`
	MaxTokens     int            `json:"max_tokens"`
	Stream        bool           `json:"stream"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

// StreamOptions contains options for streaming completions. It is an extended version of the OpenAI StreamOptions type.
type StreamOptions struct {
	IncludeUsage         bool `json:"include_usage"`
	ContinuousUsageStats bool `json:"continuous_usage_stats"`
}

// EncryptedChatResponse is the response structure for an OpenAI chat completion call,
// with an encrypted "choices" field.
//
// Don't send the marshalled type to clients/servers. Read package docs for more info.
type EncryptedChatResponse struct {
	// Choices is the whole choices array from Response as an encrypted blob.
	// The field is marked as omitempty to avoid adding empty strings in unit tests.
	Choices string `json:"choices,omitempty"`
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int    `json:"created"`
	Model   string `json:"model"`
	Usage   Usage  `json:"usage"`
}

// ChatResponse is the response structure for an OpenAI chat completion call.
//
// Don't send the marshalled type to clients/servers. Read package docs for more info.
type ChatResponse struct {
	Choices []choice `json:"choices"`
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
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int    `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type choice struct {
	Index        int     `json:"index"`
	Message      message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Usage contains the token usage of an OpenAI chat completion call.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}
