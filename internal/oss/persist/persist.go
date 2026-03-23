// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package persist provides utilities for reading HTTP request bodies into memory.
// It replaces the original request body with an in-memory buffer, enabling repeated reads.
package persist

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// persistedBody represents a [http.Request.Body] which is fully in memory.
// Create or retrieve its bytes from a request via [ReadBody] and similar functions.
type persistedBody struct {
	r     *bytes.Reader
	bytes []byte
}

func setReplayableBody(r *http.Request, body []byte) {
	r.ContentLength = int64(len(body))
	r.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	r.Body = &persistedBody{r: bytes.NewReader(body), bytes: body}
}

func (p *persistedBody) Read(b []byte) (int, error) {
	return p.r.Read(b)
}

func (p *persistedBody) Close() error {
	return nil
}

// ReadBody reads the body of r into memory and replaces its [http.Request.Body] to be fully readable again.
// The body of r must not have been read beforehand.
// maxBytes is enforced via [http.MaxBytesReader], and if exceeded the response is informed.
func ReadBody(w http.ResponseWriter, r *http.Request, maxBytes int64) ([]byte, error) {
	// Fast path: Body is already persisted
	if persisted, ok := r.Body.(*persistedBody); ok {
		setReplayableBody(r, persisted.bytes)
		return persisted.bytes, nil
	}

	maxBodyReader := http.MaxBytesReader(w, r.Body, maxBytes)
	bodyBytes, err := io.ReadAll(maxBodyReader)
	_ = maxBodyReader.Close()
	if err != nil {
		return nil, err
	}

	setReplayableBody(r, bodyBytes)
	return bodyBytes, nil
}

// CloneRequest performs a deep clone on r including its body via [ReadBody].
// The request r is left intact and its body is fully readable again from memory.
func CloneRequest(w http.ResponseWriter, r *http.Request, maxBytes int64) (*http.Request, error) {
	body, err := ReadBody(w, r, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	// Clone() clones deeply, except Body
	cloned := r.Clone(r.Context())
	setReplayableBody(cloned, body)

	return cloned, nil
}

// ReadBodyUnlimited reads the body of r into memory and replaces its [http.Request.Body] to be fully
// readable again. The body of r must not have been read beforehand. There is no limit on the
// the number of bytes read into memory.
func ReadBodyUnlimited(r *http.Request) ([]byte, error) {
	// Fast path: Body is already persisted
	if persisted, ok := r.Body.(*persistedBody); ok {
		setReplayableBody(r, persisted.bytes)
		return persisted.bytes, nil
	}

	bodyBytes, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		return nil, err
	}

	setReplayableBody(r, bodyBytes)
	return bodyBytes, nil
}

// CloneRequestUnlimited performs a deep clone on r including its body via [ReadBodyUnlimited].
// The request r is left intact and its body is fully readable again from memory.
func CloneRequestUnlimited(r *http.Request) (*http.Request, error) {
	body, err := ReadBodyUnlimited(r)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	// Clone() clones deeply, except Body
	cloned := r.Clone(r.Context())
	setReplayableBody(cloned, body)
	return cloned, nil
}

// SetBody changes the [http.Request.Body] of r to body.
// The body bytes can be extracted again via [ReadBody].
func SetBody(r *http.Request, body []byte) {
	setReplayableBody(r, body)
}
