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
		decryptedMsgs, id, requestNonce, err := getDecryptedMessages(secrets, r, w)
		if err != nil {
			log.Error("Decrypting message", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		lastMsg := decryptedMsgs[len(decryptedMsgs)-1].Content
		responseMsg := fmt.Sprintf("Echo: %s", lastMsg)
		choices := []choice{
			{
				Index: 0,
				Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{
					Role:    "assistant",
					Content: responseMsg,
				},
				FinishReason: "stop",
			},
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
		openAIResp := openai.EncryptedChatResponse{
			ID:      "chatcmpl-123", // This should be dynamically generated in a real implementation
			Object:  "chat.completion",
			Created: int(time.Now().Unix()),
			Model:   "gpt",
			Choices: encryptedResponseMsg,
			Usage: openai.Usage{
				PromptTokens:     len(lastMsg),
				CompletionTokens: len(responseMsg),
				TotalTokens:      len(lastMsg) + len(responseMsg),
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

func getDecryptedMessages(secrets map[string][]byte, r *http.Request, _ http.ResponseWriter) (msgs []message, id string, nonce []byte, err error) {
	var openAIReq map[string]interface{}
	err = json.NewDecoder(r.Body).Decode(&openAIReq)
	if err != nil {
		return nil, "", nil, err
	}
	msg := openAIReq["messages"].(string)
	id, err = crypto.GetIDFromCipher(msg)
	if err != nil {
		return nil, "", nil, err
	}
	secret, ok := secrets[id]
	if !ok {
		return nil, id, nil, fmt.Errorf("secret ID %s not found", id)
	}
	nonce, err = crypto.GetNonceFromCipher(msg)
	if err != nil {
		return nil, id, nil, err
	}
	decryptedMsgRaw, err := crypto.DecryptMessage(msg, secret, nonce, 0)
	if err != nil {
		return nil, id, nil, err
	}

	var decryptedMsg []message
	err = json.Unmarshal([]byte(decryptedMsgRaw), &decryptedMsg)
	return decryptedMsg, id, nonce, err
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
