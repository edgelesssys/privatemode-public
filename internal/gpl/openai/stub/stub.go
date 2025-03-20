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
		decryptedRequest, id, requestNonce, err := decryptRequest(secrets, r, w)
		if err != nil {
			log.Error("Decrypting request", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		decryptedMsgs, ok := decryptedRequest["messages"].([]message)
		if !ok {
			http.Error(w, "Reading messages", http.StatusInternalServerError)
			return
		}
		lastMsgContent := decryptedMsgs[len(decryptedMsgs)-1].Content
		var responseMsg string
		if lastMsgContent != nil {
			responseMsg = fmt.Sprintf("Echo: %s", *lastMsgContent)
		} else {
			responseMsg = "Echo: nil"
		}

		choices := []choice{
			{
				Index: 0,
				Message: message{
					Role:      "assistant",
					Content:   &responseMsg,
					ToolCalls: nil,
				},
				FinishReason: "stop",
			},
		}

		// if there were tools set, we call all of them, returning the entire tool json with a test field set to "echo"
		tools := decryptedRequest["tools"]
		if tools != nil {
			decryptedTools := tools.([]interface{})
			toolCalls := make([]string, len(decryptedTools))
			for i, tool := range decryptedTools {
				toolMap := tool.(map[string]interface{})
				toolMap["test"] = "echo"
				toolJSON, _ := json.Marshal(toolMap)
				toolCalls[i] = string(toolJSON)
			}
			choices[0].Message.ToolCalls = toolCalls
		}

		marshalledResp, err := json.Marshal(choices)
		if err != nil {
			log.Error("Unmarshalling json", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		secret, ok := secrets[id]
		if !ok {
			http.Error(w, fmt.Sprintf("unknown secret ID %s", id), http.StatusInternalServerError)
		}
		encryptedResponseMsg, err := crypto.EncryptMessage(string(marshalledResp), secret, id, requestNonce, 0)
		if err != nil {
			log.Error("Encrypting message", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		inputTokens := 0
		for _, msg := range decryptedMsgs {
			if msg.Content != nil {
				inputTokens += len(*msg.Content)
			}
			if msg.ToolCalls != nil {
				for _, toolCall := range msg.ToolCalls {
					inputTokens += len(toolCall)
				}
			}
		}
		openAIResp := openai.EncryptedChatResponse{
			ID:      "chatcmpl-123", // This should be dynamically generated in a real implementation
			Object:  "chat.completion",
			Created: int(time.Now().Unix()),
			Model:   "gpt",
			Choices: encryptedResponseMsg,
			Usage: openai.Usage{
				PromptTokens:     inputTokens,
				CompletionTokens: len(responseMsg),
				TotalTokens:      inputTokens + len(responseMsg),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(openAIResp); err != nil {
			log.Error("Encoding response", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func decryptRequest(secrets map[string][]byte, r *http.Request, _ http.ResponseWriter) (req map[string]interface{}, id string, nonce []byte, err error) {
	var openAIReq map[string]interface{}
	err = json.NewDecoder(r.Body).Decode(&openAIReq)
	if err != nil {
		return openAIReq, "", nil, err
	}

	decryptedMsgRaw, id, nonce, err := decryptField(secrets, openAIReq["messages"].(string), 0)
	if err != nil {
		return openAIReq, id, nonce, err
	}
	var decryptedMsg []message
	err = json.Unmarshal([]byte(*decryptedMsgRaw), &decryptedMsg)
	if err != nil {
		return openAIReq, id, nonce, err
	}
	openAIReq["messages"] = decryptedMsg

	// if tools are there, decrypt them
	if tools, ok := openAIReq["tools"]; ok {
		decryptedToolsRaw, id, nonce, err := decryptField(secrets, tools.(string), 1)
		if err != nil {
			return openAIReq, id, nonce, err
		}
		var decryptedTools []interface{}
		if err := json.Unmarshal([]byte(*decryptedToolsRaw), &decryptedTools); err != nil {
			return openAIReq, id, nonce, err
		}

		openAIReq["tools"] = decryptedTools
	}

	return openAIReq, id, nonce, err
}

func decryptField(secrets map[string][]byte, encrypted string, sequenceNumber uint32) (decrypted *string, id string, nonce []byte, err error) {
	id, err = crypto.GetIDFromCipher(encrypted)
	if err != nil {
		return nil, "", nil, err
	}
	secret, ok := secrets[id]
	if !ok {
		return nil, id, nil, fmt.Errorf("secret ID %s not found", id)
	}
	nonce, err = crypto.GetNonceFromCipher(encrypted)
	if err != nil {
		return nil, id, nonce, err
	}
	decryptedMsgRaw, err := crypto.DecryptMessage(encrypted, secret, nonce, sequenceNumber)
	if err != nil {
		return nil, id, nonce, err
	}

	return &decryptedMsgRaw, id, nonce, nil
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
