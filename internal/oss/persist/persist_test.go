// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package persist

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testBody = []byte(`{"model":"test-model","prompt":"hello world"}`)

func TestReadBody(t *testing.T) {
	t.Run("returns correct bytes and is idempotent", func(t *testing.T) {
		r := newRequest(t, testBody)
		w := httptest.NewRecorder()

		read1, err := ReadBody(w, r, int64(len(testBody)+1))
		require.NoError(t, err)
		assert.Equal(t, testBody, read1)

		read2, err := ReadBody(w, r, int64(len(testBody)+1))
		require.NoError(t, err)
		assert.Equal(t, testBody, read2)

		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, testBody, bodyBytes)
	})

	t.Run("respects maxBytes limit", func(t *testing.T) {
		r := newRequest(t, testBody)
		w := httptest.NewRecorder()

		_, err := ReadBody(w, r, 5)
		require.Error(t, err)

		var maxBytesErr *http.MaxBytesError
		assert.ErrorAs(t, err, &maxBytesErr)
	})
}

func TestSetBody(t *testing.T) {
	t.Run("replaces body and updates ContentLength", func(t *testing.T) {
		r := newRequest(t, testBody)
		replacement := []byte(`{"replaced":true}`)

		SetBody(r, replacement)

		assert.Equal(t, int64(len(replacement)), r.ContentLength)

		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, replacement, bodyBytes)
	})
}

func TestCloneRequest(t *testing.T) {
	t.Run("clone has independent body", func(t *testing.T) {
		r := newRequest(t, testBody)
		w := httptest.NewRecorder()

		cloned, err := CloneRequest(w, r, int64(len(testBody)+1))
		require.NoError(t, err)

		clonedBody, err := io.ReadAll(cloned.Body)
		require.NoError(t, err)
		assert.Equal(t, testBody, clonedBody)

		origBody, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, testBody, origBody)
	})
}

func newRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodPost, "/v1/test", bytes.NewReader(bytes.Clone(body)))
}
