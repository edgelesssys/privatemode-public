// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package anthropic

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMediaContentValidator(t *testing.T) {
	newRequest := func(content []map[string]any) *http.Request {
		t.Helper()
		data, err := json.Marshal(MessagesRequest{
			Messages: []Message{
				{Role: "user", Content: content},
			},
		})
		if err != nil {
			panic(err)
		}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
			"https://foo.bar/v1/messages", bytes.NewReader(data))
		if err != nil {
			panic(err)
		}
		return req
	}

	testCases := map[string]struct {
		request     *http.Request
		validateErr func(error) bool
	}{
		"https url": {
			request: newRequest([]map[string]any{
				{
					"type": "image",
					"source": map[string]string{
						"type": "url",
						"url":  "https://example.com/image.jpg",
					},
				},
			}),
			validateErr: func(err error) bool { return err == nil },
		},
		"http url": {
			request: newRequest([]map[string]any{
				{
					"type": "image",
					"source": map[string]string{
						"type": "url",
						"url":  "http://example.com/image.jpg",
					},
				},
			}),
			validateErr: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), "insecure")
			},
		},
		"file url": {
			request: newRequest([]map[string]any{
				{
					"type": "image",
					"source": map[string]string{
						"type": "url",
						"url":  "file:///var/lib/image.png",
					},
				},
			}),
			validateErr: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), "insecure")
			},
		},
		"data source": {
			request: newRequest([]map[string]any{
				{
					"type": "image",
					"source": map[string]string{
						"type":       "base64",
						"media_type": "image/jpeg",
						"data":       "/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAAYEBQYFBAYGBQYHBwYIChAKCgkJChQODwwQFxQYGBcUFhYaHSUfGhsjHBYWICwgIyYnKSopGR8tMC0oMCUoKSj/2wBDAQcHBwoIChMKChMoGhYa",
					},
				},
			}),
			validateErr: func(err error) bool { return err == nil },
		},
		"data url": {
			// Does not conform to Anthropic API but vLLM allows it
			request: newRequest([]map[string]any{
				{
					"type": "image",
					"source": map[string]string{
						"type": "url",
						"url":  "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAAYEBQYFBAYGBQYHBwYIChAKCgkJChQODwwQFxQYGBcUFhYaHSUfGhsjHBYWICwgIyYnKSopGR8tMC0oMCUoKSj/2wBDAQcHBwoIChMKChMoGhYa",
					},
				},
			}),
			validateErr: func(err error) bool { return err == nil },
		},
		"http url as second content block": {
			request: newRequest([]map[string]any{
				{
					"type": "image",
					"source": map[string]string{
						"type": "url",
						"url":  "https://example.com/image.jpg",
					},
				},
				{
					"type": "image",
					"source": map[string]string{
						"type": "url",
						"url":  "http://example.com/image.jpg",
					},
				},
			}),
			validateErr: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), "insecure")
			},
		},
		"tool_result/https url": {
			request: newRequest([]map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_abc123",
					"content": []map[string]any{
						{
							"type": "image",
							"source": map[string]string{
								"type": "url",
								"url":  "https://example.com/image.jpg",
							},
						},
					},
				},
			}),
			validateErr: func(err error) bool { return err == nil },
		},
		"tool_result/http url": {
			request: newRequest([]map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_abc123",
					"content": []map[string]any{
						{
							"type": "image",
							"source": map[string]string{
								"type": "url",
								"url":  "http://example.com/image.jpg",
							},
						},
					},
				},
			}),
			validateErr: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), "insecure")
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			mutator := MediaContentValidator(logger)

			err := mutator(tc.request)
			require.True(tc.validateErr(err))
		})
	}
}
