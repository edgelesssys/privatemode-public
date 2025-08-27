// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package openai

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

func TestSecureImageURLValidator(t *testing.T) {
	newRequest := func(body any) *http.Request {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			panic(err)
		}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
			"https://foo.bar/v1/chat/completions", bytes.NewReader(data))
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
			request: newRequest(ChatRequest{
				Messages: []Message{
					{
						Role: "user",
						Content: []map[string]string{
							{
								"type":      "input_image",
								"image_url": "https://example.com/image.jpg",
							},
						},
					},
				},
			}),
			validateErr: func(err error) bool { return err == nil },
		},
		"http url": {
			request: newRequest(ChatRequest{
				Messages: []Message{
					{
						Role: "user",
						Content: []map[string]string{
							{
								"type":      "input_image",
								"image_url": "http://example.com/image.jpg",
							},
						},
					},
				},
			}),
			validateErr: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), "insecure")
			},
		},
		"data url": {
			request: newRequest(ChatRequest{
				Messages: []Message{
					{
						Role: "user",
						Content: []map[string]string{
							{
								"type":      "input_image",
								"image_url": "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAAYEBQYFBAYGBQYHBwYIChAKCgkJChQODwwQFxQYGBcUFhYaHSUfGhsjHBYWICwgIyYnKSopGR8tMC0oMCUoKSj/2wBDAQcHBwoIChMKChMoGhYa",
							},
						},
					},
				},
			}),
			validateErr: func(err error) bool { return err == nil },
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			mutator := SecureImageURLValidator(logger)

			err := mutator(tc.request)
			require.True(tc.validateErr(err))
		})
	}
}
