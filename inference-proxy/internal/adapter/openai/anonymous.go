package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/inference"
	"github.com/edgelesssys/continuum/internal/anonymous"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/persist"
)

// validateAnonymousRequest restricts anonymous OpenAI chat requests after
// decryption and token validation, before the other API-specific mutations.
func validateAnonymousRequest(r *http.Request) error {
	if r.Header.Get(constants.PrivatemodeAnonymousAuthHeader) == "" {
		return nil
	}
	deny := func(message string) error {
		return forwarder.NewError("anonymous access", errors.New(message+"; sign in to continue"), http.StatusForbidden)
	}
	if r.URL.Path != constants.OpenAIChatCompletionsEndpoint {
		return inference.RejectAnonymousRequest(r)
	}
	body, err := persist.ReadBodyUnlimited(r)
	if err != nil {
		return fmt.Errorf("reading anonymous request: %w", err)
	}
	// Accept the app's request shape only. In particular, tools, custom chat
	// templates, extra message fields and multimodal content cannot add uncounted
	// input or replace the system prompt. Unknown fields fail closed.
	var request struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream          bool   `json:"stream,omitempty"`
		CacheSalt       string `json:"cache_salt,omitempty"`
		ReasoningEffort string `json:"reasoning_effort,omitempty"`
		StreamOptions   *struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options,omitempty"`
		ChatTemplateKwargs *struct {
			Thinking       *bool `json:"thinking,omitempty"`
			EnableThinking *bool `json:"enable_thinking,omitempty"`
		} `json:"chat_template_kwargs,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if !json.Valid(body) || decoder.Decode(&request) != nil {
		return deny("only app text chat requests are available")
	}
	modelName, ok := anonymous.Policy.Models[request.Model]
	if !ok {
		return deny("model is not available anonymously")
	}
	if len(request.Messages) < 2 || request.Messages[0].Role != "system" {
		return deny("the app system prompt is required")
	}
	validSystemPrompt := false
	for _, template := range anonymous.Policy.SystemPrompts {
		if request.Messages[0].Content == strings.ReplaceAll(template, "{model}", modelName) {
			validSystemPrompt = true
			break
		}
	}
	if !validSystemPrompt {
		return deny("the app system prompt is required")
	}
	characters := 0
	for _, message := range request.Messages[1:] {
		if message.Role != "user" && message.Role != "assistant" {
			return deny("only user and assistant messages may follow the app system prompt")
		}
		characters += utf8.RuneCountInString(message.Content)
	}
	if characters > anonymous.Policy.MaxCharacters {
		return deny(fmt.Sprintf("conversation text must not exceed %d characters", anonymous.Policy.MaxCharacters))
	}
	// Forward exactly what was validated. JSON parsers differ in their handling
	// of duplicate keys and casing; forwarding the original could bypass limits.
	validatedBody, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encoding anonymous request: %w", err)
	}
	persist.SetBody(r, validatedBody)
	return nil
}
