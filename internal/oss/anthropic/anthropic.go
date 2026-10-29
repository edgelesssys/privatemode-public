// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package anthropic provides type definitions for the Anthropic Messages API.
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
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/mutators"
	"github.com/edgelesssys/continuum/internal/oss/usage"
	"github.com/tidwall/gjson"
)

// MessagesRequest is the request structure for an Anthropic messages call.
//
// Don't send the marshalled type to clients/servers. Read package docs for more info.
type MessagesRequest struct {
	Model     string          `json:"model"`
	Stream    bool            `json:"stream,omitzero"`
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

// ToUsageStats converts an Anthropic [Usage] to a [usage.Stats].
func (u Usage) ToUsageStats() usage.Stats {
	return usage.Stats{
		PromptTokens:       int64(u.InputTokens + u.CacheCreationInputTokens),
		CachedPromptTokens: int64(u.CacheReadInputTokens),
		CompletionTokens:   int64(u.OutputTokens),
	}
}

// MediaContentValidator creates a [forwarder.RequestMutator] that enforces policy on media content
// blocks in the request. Image URLs must use data scheme via [validateImageURL].
func MediaContentValidator(log *slog.Logger) forwarder.RequestMutator {
	validate := func(httpBody string) (mutatedRequest string, err error) {
		// Skip empty body, e.g., for OPTIONS requests
		if len(httpBody) == 0 {
			return httpBody, nil
		}

		// Images with URL source
		if err := validateStringQueryResults(httpBody, []string{
			// Images in message content:
			`messages.#.content.#(type=="image")#.source.url|@flatten`,
			// Images embedded in tool result content:
			`messages.#.content.#(type=="tool_result")#.content.#(type=="image")#.source.url|@flatten|@flatten`,
		}, validateImageURL); err != nil {
			return "", forwarder.NewError("validating image URLs", err, http.StatusBadRequest)
		}

		// Images may also be passed as type base64 and this is allowed:
		// "source": { "type": "base64", "media_type": "image/png", "data": "iVBORw0KGgo..." }

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
		// The parse error quotes the URL, which is request content.
		return errors.New("parsing image URL failed")
	}

	if !strings.EqualFold(parsedURL.Scheme, "data") {
		return mutators.ErrRemoteResourceURL
	}
	return nil
}
