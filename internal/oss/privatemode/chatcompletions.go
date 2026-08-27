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
	reader *sse.EventReader
	body   io.ReadCloser
	done   bool
	// Header contains the decrypted response headers.
	Header http.Header
}

// ChatCompletions sends an encrypted chat completions request and
// returns the decrypted response.
func (c *Client) ChatCompletions(ctx context.Context, body []byte) ([]byte, error) {
	resp, err := c.ChatCompletionsWithOptions(ctx, body, RequestOptions{})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// ChatCompletionsWithOptions sends an encrypted chat completions request and
// preserves the response headers.
func (c *Client) ChatCompletionsWithOptions(
	ctx context.Context,
	body []byte,
	opts RequestOptions,
) (*Response, error) {
	req, err := c.prepareChatCompletionsRequest(ctx, body, opts)
	if err != nil {
		return nil, err
	}

	resp, err := c.doEncryptedAPIRequest(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	decrypted, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading decrypted response: %w", err)
	}

	return &Response{Header: resp.Header.Clone(), Body: decrypted}, nil
}

// StreamChatCompletions sends an encrypted chat completions request
// with streaming enabled and returns a stream of decrypted response
// chunks.
//
// The request body should include `"stream": true“. The caller must
// close the returned stream when done.
func (c *Client) StreamChatCompletions(ctx context.Context, body []byte) (*ChatCompletionsStream, error) {
	return c.StreamChatCompletionsWithOptions(ctx, body, RequestOptions{})
}

// StreamChatCompletionsWithOptions sends an encrypted streaming chat
// completions request and preserves the response headers.
func (c *Client) StreamChatCompletionsWithOptions(
	ctx context.Context,
	body []byte,
	opts RequestOptions,
) (*ChatCompletionsStream, error) {
	req, err := c.prepareChatCompletionsRequest(ctx, body, opts)
	if err != nil {
		return nil, err
	}

	resp, err := c.doEncryptedAPIRequest(req)
	if err != nil {
		return nil, err
	}

	if !strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
		resp.Body.Close()
		return nil, fmt.Errorf("expected streaming response (text/event-stream), got %q", resp.Header.Get("Content-Type"))
	}

	return &ChatCompletionsStream{
		reader: sse.NewEventReader(resp.Body, constants.MaxSSEEventBytes),
		body:   resp.Body,
		Header: resp.Header.Clone(),
	}, nil
}

// prepareChatCompletionsRequest builds the plain HTTP request and applies
// the relevant request mutators.
func (c *Client) prepareChatCompletionsRequest(
	ctx context.Context,
	body []byte,
	opts RequestOptions,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header = cloneRequestHeader(opts.Header)
	req.Header.Set("Content-Type", "application/json")

	chatModelExtractor := func(req *http.Request) (string, error) {
		body, err := persist.ReadBodyUnlimited(req)
		if err != nil {
			return "", fmt.Errorf("reading request body: %w", err)
		}

		var request struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			return "", fmt.Errorf("parsing chat request: %w", err)
		}
		return request.Model, nil
	}
	mutator := forwarder.RequestMutatorChain(
		mutators.ShardKeyInjector(c.promptCacheSalt),
		openai.CacheSaltInjector(func() string { return c.promptCacheSalt }, c.log),
		mutators.ModelHeaderInjector(chatModelExtractor),
	)
	if err := mutator(req); err != nil {
		return nil, fmt.Errorf("mutating request: %w", err)
	}

	return req, nil
}

// Next returns the next decrypted chunk from the stream.
// Returns [io.EOF] when the stream is complete.
func (s *ChatCompletionsStream) Next() ([]byte, error) {
	if s.done {
		return nil, io.EOF
	}

	for {
		event, err := s.reader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}

		if !event.Dispatches() {
			continue
		}

		data := event.FullData()
		if openai.IsStreamDone(data) {
			s.done = true
			return nil, io.EOF
		}

		return data, nil
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
	return s.body.Close()
}
