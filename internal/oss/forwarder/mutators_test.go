// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package forwarder

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		"validation error": {
			mutator: stubMutator{
				mutateErr: NewError("validation failed", errors.New("invalid input"), http.StatusBadRequest),
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
				assert.ErrorIs(err, tc.mutator.mutateErr)
				assert.Equal(StatusFromError(tc.mutator.mutateErr), StatusFromError(err))
				return
			}

			assert.NoError(err)
			assert.Equal(http.StatusOK, StatusFromError(err))
			body, err := io.ReadAll(request.Body)
			assert.NoError(err)
			assert.Equal(tc.expectedResponse, string(body))
		})
	}
}

func TestWithFormRequestMutationParseErrorOmitsBody(t *testing.T) {
	body := "--b\r\nSECRET-MARKER\r\n\r\nvalue\r\n--b--\r\n"
	r, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(body))
	require.NoError(t, err)
	r.Header.Set("Content-Type", "multipart/form-data; boundary=b")

	mutator := WithFormRequestMutation(func(*multipart.Form, *multipart.Writer) (bool, error) {
		t.Error("mutate must not be called for a malformed form")
		return false, nil
	}, slog.New(slog.DiscardHandler))

	err = mutator(r)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SECRET-MARKER")
}

func TestRequestMutatorChainError(t *testing.T) {
	wantErr := errors.New("invalid request")
	mutator := RequestMutatorChain(
		NoRequestMutation,
		func(*http.Request) error {
			return NewError("validation failed", wantErr, http.StatusBadRequest)
		},
		func(*http.Request) error {
			t.Error("mutator called after an error")
			return nil
		},
	)
	err := mutator(&http.Request{})
	assert.Equal(t, http.StatusBadRequest, StatusFromError(err))
	assert.ErrorIs(t, err, wantErr)
}

type stubMutator struct {
	mutateResponse string
	mutateErr      error
}

func (s stubMutator) mutate(_ string) (string, error) {
	return s.mutateResponse, s.mutateErr
}
