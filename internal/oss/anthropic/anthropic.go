// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package anthropic provides type definitions and field selectors for the Anthropic Messages API.
// The marshaled value of the types defined here should never be returned to
// a client, or passed to the model backend.
// This is important to avoid dropping unknown fields sent by the client/model,
// or setting optional fields that are not expected by the client/model.
// For safe modification of the requests/responses, use [sjson.SetBytes] instead.
//
// [sjson.SetBytes]: https://pkg.go.dev/github.com/tidwall/sjson#SetBytes
package anthropic

import (
	"encoding/json"

	"github.com/edgelesssys/continuum/internal/oss/forwarder"
)

const (
	// MessagesEndpoint is the endpoint for creating messages.
	MessagesEndpoint = "/v1/messages"
)

// PlainMessagesRequestFields is a field selector for all fields in an Anthropic messages request that are NOT encrypted.
// All other fields (messages, system, tools, etc.) will be encrypted.
var PlainMessagesRequestFields = forwarder.FieldSelector{
	{"model"},
	{"stream"},
}

// PlainMessagesResponseFields is a field selector for all fields in an Anthropic messages response that are NOT encrypted.
// All other fields (content, etc.) will be encrypted.
// The "type" field is included to allow the apigateway to identify message_delta events for token tracking.
var PlainMessagesResponseFields = forwarder.FieldSelector{
	{"id"},
	{"type"},
	{"usage"},
}

// MessagesRequestPlainData contains fields that are not encrypted for [MessagesRequest].
// This is used by the apigateway to parse the model name from requests without decrypting the full body.
type MessagesRequestPlainData struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream,omitzero"`
}

// MessagesRequest is the request structure for an Anthropic messages call.
//
// Don't send the marshalled type to clients/servers. Read package docs for more info.
type MessagesRequest struct {
	MessagesRequestPlainData
	MaxTokens int             `json:"max_tokens,omitzero"`
	Messages  []Message       `json:"messages"`
	System    string          `json:"system,omitempty"`
	Tools     json.RawMessage `json:"tools,omitempty"`
	CacheSalt string          `json:"cache_salt,omitempty"`
}

// Message is a message in an Anthropic messages call.
type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// MessagesResponse is the response structure for an Anthropic messages call.
//
// Don't send the marshalled type to clients/servers. Read package docs for more info.
type MessagesResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role,omitempty"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model,omitempty"`
	StopReason   string         `json:"stop_reason,omitempty"`
	StopSequence string         `json:"stop_sequence,omitempty"`
	Usage        Usage          `json:"usage,omitempty"`
}

// ContentBlock represents a block of content in an Anthropic response.
type ContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	ID    string          `json:"id,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// Usage contains the token usage of an Anthropic messages call.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitzero"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitzero"`
}
