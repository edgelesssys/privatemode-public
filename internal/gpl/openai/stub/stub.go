// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// package stub implements a stubbed OpenAI API handler.
package stub

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	crypto "github.com/edgelesssys/continuum/internal/gpl/crypto"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/internal/gpl/openai"
)

// OpenAIEchoHandler returns an http.Handler that stubs an OpenAI API completion endpoint.
func OpenAIEchoHandler(secrets map[string][]byte, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", openAIHandler(secrets, log))
	mux.HandleFunc("GET /v1/models", openAIModelsHandler())
	mux.HandleFunc("OPTIONS /", func(_ http.ResponseWriter, _ *http.Request) {})
	return mux
}

func openAIHandler(secrets map[string][]byte, log *slog.Logger) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		requestMutator, responseMutator := getConnectionMutators(secrets, log)
		if err := requestMutator(r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var request openai.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if len(request.Messages) == 0 {
			http.Error(w, "no messages in request", http.StatusBadRequest)
			return
		}

		lastMsgContent := request.Messages[len(request.Messages)-1].Content
		var responseMsg string
		if lastMsgContent != nil {
			responseMsg = fmt.Sprintf("Echo: %s", *lastMsgContent)
		} else {
			responseMsg = "Echo: nil"
		}

		choices := []openai.Choice{
			{
				Index: 0,
				Message: openai.Message{
					Role:      "assistant",
					Content:   &responseMsg,
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
				PromptTokens:     inputTokens,
				CompletionTokens: len(responseMsg),
				TotalTokens:      inputTokens + len(responseMsg),
			},
		}

		responseJSON, err := json.Marshal(openAIResp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		response, err := responseMutator.Mutate(responseJSON)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := w.Write(response); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

// GetEncryptionFunctions returns the encryption and decryption functions for the given secrets.
// It uses the first secret found in the map for both encryption and decryption.
func GetEncryptionFunctions(secrets map[string][]byte) (encryptFunc forwarder.MutationFunc, decryptFunc forwarder.MutationFunc) {
	encSeqNumber, decSeqNumber := uint32(0), uint32(0)
	var id string
	var nonce []byte
	var secret []byte

	decrypt := func(cipherText string) (string, error) {
		var err error
		id, err = crypto.GetIDFromCipher(cipherText)
		if err != nil {
			return "", err
		}
		var ok bool
		secret, ok = secrets[id]
		if !ok {
			return "", fmt.Errorf("no secret for ID %q", id)
		}
		nonce, err = crypto.GetNonceFromCipher(cipherText)
		if err != nil {
			return "", err
		}

		plainText, err := crypto.DecryptMessage(cipherText, secret, nonce, decSeqNumber)
		if err != nil {
			return "", err
		}
		decSeqNumber++
		return plainText, nil
	}

	encrypt := func(plainText string) (string, error) {
		cipherText, err := crypto.EncryptMessage(plainText, secret, id, nonce, encSeqNumber)
		if err != nil {
			return "", err
		}
		encSeqNumber++
		return cipherText, nil
	}

	return encrypt, decrypt
}

func getConnectionMutators(secrets map[string][]byte, log *slog.Logger) (requestMutator forwarder.RequestMutator, responseMutator forwarder.ResponseMutator) {
	encrypt, decrypt := GetEncryptionFunctions(secrets)

	return forwarder.WithFullJSONRequestMutation(decrypt, openai.PlainRequestFields, log),
		forwarder.WithFullJSONResponseMutation(encrypt, openai.PlainResponseFields)
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
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(models); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
