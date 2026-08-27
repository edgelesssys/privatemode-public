// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package forwarder

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithRequestMutation(t *testing.T) {
	testCases := map[string]struct {
		mutator          stubMutator
		requestBody      string
		expectedResponse string
		contentType      string
		wantErr          bool
	}{
		"plain text mutation": {
			mutator: stubMutator{
				mutateResponse: "mutated text",
			},
			requestBody:      "original text",
			expectedResponse: "mutated text",
			contentType:      "text/plain",
		},
		"JSON body mutation": {
			mutator: stubMutator{
				mutateResponse: `mutated`,
			},
			requestBody:      `{"field": "original"}`,
			expectedResponse: `mutated`,
			contentType:      "application/json",
		},
		"empty body": {
			mutator: stubMutator{
				mutateResponse: "",
			},
			requestBody:      "",
			expectedResponse: "",
			contentType:      "text/plain",
		},
		"mutation error": {
			mutator: stubMutator{
				mutateErr: errors.New("mutation failed"),
			},
			requestBody: "some data",
			wantErr:     true,
			contentType: "text/plain",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mutate := WithRequestMutation(tc.mutator.mutate, slog.Default())

			request := &http.Request{
				Header: make(http.Header),
				Body:   io.NopCloser(bytes.NewBufferString(tc.requestBody)),
			}
			request.Header.Set("Content-Type", tc.contentType)

			err := mutate(request)
			if tc.wantErr {
				assert.Error(err)
				return
			}

			assert.NoError(err)
			body, err := io.ReadAll(request.Body)
			assert.NoError(err)
			assert.Equal(tc.expectedResponse, string(body))
		})
	}
}

type stubMutator struct {
	mutateResponse string
	mutateErr      error
}

func (s stubMutator) mutate(_ string) (string, error) {
	return s.mutateResponse, s.mutateErr
}
