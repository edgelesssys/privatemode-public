// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package stub implements a stubbed OpenAI API handler.
package stub

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/encbody"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/openai"
)

// EchoHandler stubs our hosted backend (API Gateway + Inference Proxy) and echoes requests in the response.
func EchoHandler(secrets map[string][]byte, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", openAIChatCompletionsHandler(secrets, log))
	mux.HandleFunc("POST /v1/completions", openAIChatCompletionsHandler(secrets, log))

	// Handlers with empty responses: strictly speaking invalid, but enough for minimal tests
	mux.HandleFunc("POST /v1/embeddings", encryptedStaticResponseHandler(secrets, []byte("{}")))
	mux.HandleFunc("POST /v1/audio/transcriptions", encryptedStaticResponseHandler(secrets, []byte("{}")))
	mux.HandleFunc("POST /v1/messages", encryptedStaticResponseHandler(secrets, []byte("{}")))

	mux.HandleFunc("GET /v1/models", openAIModelsHandler())
	return mux
}

func encryptedStaticResponseHandler(secrets map[string][]byte, response []byte) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(constants.PrivatemodeSecretIDHeader)
		secret, ok := secrets[id]
		if !ok {
			forwarder.HTTPError(w, r, http.StatusInternalServerError, "no secret for ID %q", id)
			return
		}
		se, err := encbody.InitializeServerExchange(secret, r)
		if err != nil {
			forwarder.HTTPError(w, r, http.StatusInternalServerError, "%s", err.Error())
			return
		}
		if err := se.DecryptRequest(r); err != nil {
			forwarder.HTTPError(w, r, http.StatusInternalServerError, "%s", err.Error())
			return
		}
		if err := writeEncryptedResponse(w, se, response); err != nil {
			forwarder.HTTPError(w, r, http.StatusInternalServerError, "%s", err.Error())
			return
		}
	}
}

func openAIChatCompletionsHandler(secrets map[string][]byte, log *slog.Logger) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		se, err := decryptRequest(secrets, r, log)
		if err != nil {
			forwarder.HTTPError(w, r, http.StatusInternalServerError, "Forwarding request: %s", err.Error())
			return
		}

		var request openai.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Request-Cache-Salt", request.CacheSalt)

		if r.Header.Get(constants.PrivatemodeShardKeyHeader) != "" {
			w.Header().Set("Request-Shard-Key", r.Header.Get(constants.PrivatemodeShardKeyHeader))
		}

		if len(request.Messages) == 0 {
			http.Error(w, "no messages in request", http.StatusBadRequest)
			return
		}

		lastMsgContent := request.Messages[len(request.Messages)-1].Content
		var responseMsg string
		if lastMsgContent != nil {
			responseMsg = fmt.Sprintf("Echo: %s", lastMsgContent)
		} else {
			responseMsg = "Echo: nil"
		}

		choices := []openai.Choice{
			{
				Index: 0,
				Message: openai.Message{
					Role:      "assistant",
					Content:   responseMsg,
					ToolCalls: make([]any, len(request.Tools)),
				},
				FinishReason: "stop",
			},
		}

		inputTokens := 0

		// if there were tools set, we call all of them, returning the entire tool json with a test field set to "echo"
		for i, tool := range request.Tools {
			toolMap, ok := tool.(map[string]any)
			if !ok {
				http.Error(w, fmt.Sprintf("tool call %d is not a map", i), http.StatusBadRequest)
				return
			}
			toolMap["test"] = "echo"
			toolJSON, err := json.Marshal(toolMap)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			choices[0].Message.ToolCalls[i] = string(toolJSON)
			inputTokens += len(toolJSON)
		}

		openAIResp := openai.ChatResponse{
			ID:      "chatcmpl-123", // This should be dynamically generated in a real implementation
			Object:  "chat.completion",
			Created: int(time.Now().Unix()),
			Model:   "gpt",
			Choices: choices,
			Usage: openai.Usage{
				PromptTokens:        inputTokens,
				CompletionTokens:    len(responseMsg),
				TotalTokens:         inputTokens + len(responseMsg),
				PromptTokensDetails: nil,
			},
		}

		responseJSON, err := json.Marshal(openAIResp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := writeEncryptedResponse(w, se, responseJSON); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

// decryptRequest sets up a server-side encrypted exchange and mutates r to decrypt its body. The
// decrypted request is then validated like the real backend would.
func decryptRequest(secrets map[string][]byte, r *http.Request, log *slog.Logger) (*encbody.ServerExchange, error) {
	id := r.Header.Get(constants.PrivatemodeSecretIDHeader)
	secret, ok := secrets[id]
	if !ok {
		return nil, fmt.Errorf("%s %q", constants.ErrorNoSecretForID, id)
	}

	se, err := encbody.InitializeServerExchange(secret, r)
	if err != nil {
		return nil, fmt.Errorf("initializing server exchange: %w", err)
	}
	if err := se.DecryptRequest(r); err != nil {
		return nil, fmt.Errorf("decrypting request: %w", err)
	}

	validate := forwarder.RequestMutatorChain(
		openai.CacheSaltValidator(log),
		openai.MediaContentValidator(log),
	)
	if err := validate(r); err != nil {
		return nil, fmt.Errorf("validating request: %w", err)
	}

	return se, nil
}

// writeEncryptedResponse seals plaintext as a full-body-encrypted response and writes it to w.
func writeEncryptedResponse(w http.ResponseWriter, se *encbody.ServerExchange, plaintext []byte) error {
	resp := &forwarder.UnaryResponse{Header: w.Header(), Body: plaintext}
	resp.Header.Set("Content-Type", "application/json")
	if err := se.EncryptUnaryResponse(resp); err != nil {
		return fmt.Errorf("encrypting response: %w", err)
	}
	if _, err := w.Write(resp.Body); err != nil {
		return fmt.Errorf("writing response: %w", err)
	}
	return nil
}

func openAIModelsHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		models := openai.ModelsResponse{
			Object: "list",
			Data: []openai.Model{
				{
					ID:      "gpt",
					Object:  "model",
					Created: int(time.Time{}.Unix()),
					OwnedBy: "stub",
					Tasks:   nil,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(models); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
