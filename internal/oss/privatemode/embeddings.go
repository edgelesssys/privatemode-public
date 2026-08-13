// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package privatemode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/edgelesssys/continuum/internal/oss/mutators"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/persist"
)

// Embeddings sends an encrypted embeddings request and returns the decrypted response.
func (c *Client) Embeddings(ctx context.Context, body []byte) ([]byte, error) {
	resp, err := c.EmbeddingsWithOptions(ctx, body, RequestOptions{})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// EmbeddingsWithOptions sends an encrypted embeddings request and preserves the response headers.
func (c *Client) EmbeddingsWithOptions(
	ctx context.Context,
	body []byte,
	opts RequestOptions,
) (*Response, error) {
	req, err := c.prepareEmbeddingsRequest(ctx, body, opts)
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

func (c *Client) prepareEmbeddingsRequest(
	ctx context.Context,
	body []byte,
	opts RequestOptions,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL+openai.EmbeddingsEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header = cloneRequestHeader(opts.Header)
	req.Header.Set("Content-Type", "application/json")

	modelExtractor := func(req *http.Request) (string, error) {
		body, err := persist.ReadBodyUnlimited(req)
		if err != nil {
			return "", fmt.Errorf("reading request body: %w", err)
		}

		var plainData openai.EmbeddingsRequestPlainData
		if err := json.Unmarshal(body, &plainData); err != nil {
			return "", fmt.Errorf("parsing embeddings request: %w", err)
		}
		return plainData.Model, nil
	}
	if err := mutators.ModelHeaderInjector(modelExtractor)(req); err != nil {
		return nil, fmt.Errorf("mutating request: %w", err)
	}
	return req, nil
}
