// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package privatemode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/crypto"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/mutators"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/persist"
	"github.com/edgelesssys/continuum/internal/oss/sse"
)

// ChatCompletionsStream is a stream of decrypted chat completion
// chunks from a streaming chat completions request.
//
// Use [Client.StreamChatCompletions] to create one.
// The stream must be closed after use.
type ChatCompletionsStream struct {
	reader  *sse.Reader
	decrypt func([]byte) ([]byte, error)
	resp    *http.Response
	done    bool
}

// ChatCompletions sends an encrypted chat completions request and
// returns the decrypted response.
func (c *Client) ChatCompletions(ctx context.Context, body []byte) ([]byte, error) {
	req, cipher, err := c.prepareChatCompletionsRequest(ctx, body)
	if err != nil {
		return nil, err
	}

	respBody, err := c.doAPIRequestAndReadBody(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	decrypted, err := forwarder.MutateJSONFields(respBody, cipher.DecryptResponse, openai.PlainCompletionsResponseFields)
	if err != nil {
		return nil, fmt.Errorf("decrypting response: %w", err)
	}

	return decrypted, nil
}

// StreamChatCompletions sends an encrypted chat completions request
// with streaming enabled and returns a stream of decrypted response
// chunks.
//
// The request body should include `"stream": true“. The caller must
// close the returned stream when done.
func (c *Client) StreamChatCompletions(ctx context.Context, body []byte) (*ChatCompletionsStream, error) {
	req, cipher, err := c.prepareChatCompletionsRequest(ctx, body)
	if err != nil {
		return nil, err
	}

	resp, err := c.doAPIRequest(req)
	if err != nil {
		return nil, err
	}

	if !strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
		resp.Body.Close()
		return nil, fmt.Errorf("expected streaming response (text/event-stream), got %q", resp.Header.Get("Content-Type"))
	}

	return &ChatCompletionsStream{
		reader: sse.NewReader(resp.Body, constants.MaxSSELineBytes),
		decrypt: func(data []byte) ([]byte, error) {
			return forwarder.MutateJSONFields(data, cipher.DecryptResponse, openai.PlainCompletionsResponseFields)
		},
		resp: resp,
	}, nil
}

// prepareChatCompletionsRequest validates the secret, creates a cipher,
// builds the HTTP request, and applies the relevant mutators.
func (c *Client) prepareChatCompletionsRequest(ctx context.Context, body []byte) (*http.Request, *crypto.RequestCipher, error) {
	if len(c.currentSecret.Data) == 0 {
		return nil, nil, fmt.Errorf("no secret available: call Initialize() and UpdateSecret() first")
	}

	cipher, err := crypto.NewRequestCipher(c.currentSecret.Data, c.currentSecret.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("creating request cipher: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	chatModelExtractor := func(req *http.Request) (string, error) {
		body, err := persist.ReadBodyUnlimited(req)
		if err != nil {
			return "", fmt.Errorf("reading request body: %w", err)
		}

		var plainData openai.ChatRequestPlainData
		if err := json.Unmarshal(body, &plainData); err != nil {
			return "", fmt.Errorf("parsing chat request: %w", err)
		}
		return plainData.Model, nil
	}
	mutator := forwarder.RequestMutatorChain(
		mutators.ShardKeyInjector(c.promptCacheSalt, c.log),
		openai.CacheSaltInjector(func() string { return c.promptCacheSalt }, c.log),
		mutators.ModelHeaderInjector(chatModelExtractor),
		forwarder.WithJSONRequestMutation(cipher.Encrypt, openai.PlainCompletionsRequestFields, c.log),
	)
	if err := mutator(req); err != nil {
		return nil, nil, fmt.Errorf("mutating request: %w", err)
	}

	return req, cipher, nil
}

// Next returns the next decrypted chunk from the stream.
// Returns [io.EOF] when the stream is complete.
func (s *ChatCompletionsStream) Next() ([]byte, error) {
	if s.done {
		return nil, io.EOF
	}

	for {
		line, err := s.reader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}

		switch line.Type {
		case sse.LineEnd, sse.LineComment:
			continue
		case sse.LineField:
			// We only expect "data" fields.
			if !line.IsField(sse.FieldData) {
				return nil, fmt.Errorf("unexpected SSE field %q", line.Name)
			}

			if openai.IsStreamDone(line.Value) {
				s.done = true
				return nil, io.EOF
			}

			decrypted, err := s.decrypt(line.Value)
			if err != nil {
				return nil, fmt.Errorf("decrypting chunk: %w", err)
			}

			return decrypted, nil
		}
	}
}

// All returns an iterator over all decrypted chunks in the stream.
// The caller must still close the stream when done.
func (s *ChatCompletionsStream) All() iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		for {
			chunk, err := s.Next()
			if errors.Is(err, io.EOF) {
				return
			}
			if !yield(chunk, err) {
				return
			}
			if err != nil {
				return
			}
		}
	}
}

// Close closes the stream and its underlying connection.
func (s *ChatCompletionsStream) Close() error {
	return s.resp.Body.Close()
}
