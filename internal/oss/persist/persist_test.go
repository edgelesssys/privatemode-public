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

		replayedBody, err := r.GetBody()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, replayedBody.Close()) })

		replayedBytes, err := io.ReadAll(replayedBody)
		require.NoError(t, err)
		assert.Equal(t, testBody, replayedBytes)
		assert.Equal(t, int64(len(testBody)), r.ContentLength)
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

		replayedBody, err := r.GetBody()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, replayedBody.Close()) })

		replayedBytes, err := io.ReadAll(replayedBody)
		require.NoError(t, err)
		assert.Equal(t, replacement, replayedBytes)
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

		clonedReplayBody, err := cloned.GetBody()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, clonedReplayBody.Close()) })

		clonedReplayBytes, err := io.ReadAll(clonedReplayBody)
		require.NoError(t, err)
		assert.Equal(t, testBody, clonedReplayBytes)
		assert.Equal(t, int64(len(testBody)), cloned.ContentLength)

		origReplayBody, err := r.GetBody()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, origReplayBody.Close()) })

		origReplayBytes, err := io.ReadAll(origReplayBody)
		require.NoError(t, err)
		assert.Equal(t, testBody, origReplayBytes)
		assert.Equal(t, int64(len(testBody)), r.ContentLength)
	})
}

func TestCloneRequestUnlimited(t *testing.T) {
	t.Run("clone and original are replayable", func(t *testing.T) {
		r := newRequest(t, testBody)

		cloned, err := CloneRequestUnlimited(r)
		require.NoError(t, err)

		clonedBody, err := io.ReadAll(cloned.Body)
		require.NoError(t, err)
		assert.Equal(t, testBody, clonedBody)

		origBody, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, testBody, origBody)

		clonedReplayBody, err := cloned.GetBody()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, clonedReplayBody.Close()) })

		clonedReplayBytes, err := io.ReadAll(clonedReplayBody)
		require.NoError(t, err)
		assert.Equal(t, testBody, clonedReplayBytes)
		assert.Equal(t, int64(len(testBody)), cloned.ContentLength)

		origReplayBody, err := r.GetBody()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, origReplayBody.Close()) })

		origReplayBytes, err := io.ReadAll(origReplayBody)
		require.NoError(t, err)
		assert.Equal(t, testBody, origReplayBytes)
		assert.Equal(t, int64(len(testBody)), r.ContentLength)
	})
}

func newRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/test", bytes.NewReader(bytes.Clone(body)))
}
