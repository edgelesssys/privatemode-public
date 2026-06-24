// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package encbody

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPlainRequest(t *testing.T, contentType string, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/some-path", bytes.NewReader(body))
	req.Header.Set("Custom", "keepme")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

// handshake runs a request handshake so both sides share an exchange and can start a response.
func handshake(t *testing.T, secret []byte) (*ClientExchange, *ServerExchange) {
	t.Helper()
	client, err := InitializeClientExchange(secret)
	require.NoError(t, err)

	req := newPlainRequest(t, "text/plain", []byte("handshake"))
	require.NoError(t, client.EncryptRequest(req))

	server, err := InitializeServerExchange(secret, req)
	require.NoError(t, err)
	require.NoError(t, server.DecryptRequest(req))

	return client, server
}

func TestIsEncrypted(t *testing.T) {
	tests := map[string]struct {
		contentType string
		want        bool
	}{
		"encrypted":             {ContentType, true},
		"encrypted with params": {ContentType + "; v=1", true},
		"event stream":          {"text/event-stream", false},
		"absent":                {"", false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			h := http.Header{}
			if tc.contentType != "" {
				h.Set("Content-Type", tc.contentType)
			}
			assert.Equal(t, tc.want, IsEncrypted(h))
		})
	}
}

func TestEncodeDecodeOAEHeader(t *testing.T) {
	tests := map[string]struct {
		raw []byte
	}{
		"empty":   {raw: []byte{}},
		"typical": {raw: bytes.Repeat([]byte{0xAB}, 32)},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := decodeOAEHeader(encodeOAEHeader(tc.raw))
			require.NoError(t, err)
			assert.Equal(t, tc.raw, got)
		})
	}
}

func TestDecodeOAEHeaderValidation(t *testing.T) {
	tests := map[string]struct {
		value string
	}{
		"no delimiters": {value: "abcdef"},
		"too short":     {value: ":"},
		"empty":         {value: ""},
		"not base64":    {value: ":!!!!:"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := decodeOAEHeader(tc.value)
			assert.Error(t, err)
		})
	}
}

func TestRequestRoundTrip(t *testing.T) {
	cases := map[string]struct {
		contentType string
		body        []byte
	}{
		"with content-type":    {contentType: "application/json", body: []byte(`{"model":"gpt"}`)},
		"without content-type": {contentType: "", body: []byte("raw payload")},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			secret := testSecret()

			client, err := InitializeClientExchange(secret)
			require.NoError(t, err)

			req := newPlainRequest(t, tc.contentType, tc.body)
			require.NoError(t, client.EncryptRequest(req))

			// Wire-level effects of encryption.
			assert.Equal(t, ContentType, req.Header.Get("Content-Type"))
			assert.NotEmpty(t, req.Header.Get(RequestOAEHeader))
			assert.Equal(t, "keepme", req.Header.Get("Custom"))

			server, err := InitializeServerExchange(secret, req)
			require.NoError(t, err)
			require.NoError(t, server.DecryptRequest(req))

			// The server recovers the plaintext and the original header state.
			plaintext, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			assert.Equal(t, tc.body, plaintext)
			assert.Empty(t, req.Header.Values(RequestOAEHeader))
			assert.Equal(t, "keepme", req.Header.Get("Custom"))
			if tc.contentType != "" {
				assert.Equal(t, tc.contentType, req.Header.Get("Content-Type"))
			} else {
				assert.Empty(t, req.Header.Values("Content-Type"))
			}
		})
	}
}

func TestUnaryResponseRoundTrip(t *testing.T) {
	cases := map[string]struct {
		contentType string
		body        []byte
	}{
		"with content-type":    {contentType: "application/json", body: []byte(`{"answer":42}`)},
		"without content-type": {contentType: "", body: []byte("opaque")},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client, server := handshake(t, testSecret())

			unary := &forwarder.UnaryResponse{StatusCode: http.StatusOK, Header: http.Header{}, Body: tc.body}
			if tc.contentType != "" {
				unary.Header.Set("Content-Type", tc.contentType)
			}
			unary.Header.Set("Custom", "keepme")

			require.NoError(t, server.EncryptUnaryResponse(unary))
			assert.Equal(t, ContentType, unary.Header.Get("Content-Type"))
			assert.NotEmpty(t, unary.Header.Get(ResponseOAEHeader))
			assert.NotEqual(t, tc.body, unary.Body, "body must be ciphertext on the wire")

			httpResp := &http.Response{
				StatusCode: unary.StatusCode,
				Header:     unary.Header.Clone(),
				Body:       io.NopCloser(bytes.NewReader(unary.Body)),
			}
			ds, err := client.DecryptResponse(httpResp, testMaxFrameSize)
			require.NoError(t, err)

			assert.Equal(t, http.StatusOK, ds.StatusCode)
			assert.Empty(t, ds.Header.Values(ResponseOAEHeader))
			assert.Equal(t, "keepme", ds.Header.Get("Custom"))
			if tc.contentType != "" {
				assert.Equal(t, tc.contentType, ds.Header.Get("Content-Type"))
			} else {
				assert.Empty(t, ds.Header.Values("Content-Type"))
			}

			plaintext, err := io.ReadAll(ds.Body)
			require.NoError(t, err)
			require.NoError(t, ds.Body.Close())
			assert.Equal(t, tc.body, plaintext)
		})
	}
}

func TestSSEResponseRoundTrip(t *testing.T) {
	client, server := handshake(t, testSecret())

	const sseStream = "data: hello\n\ndata: world\n\n"
	resp := &forwarder.StreamingResponse{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(sseStream)),
	}
	resp.Header.Set("Content-Type", "text/event-stream")
	resp.Header.Set("Custom", "keepme")

	require.NoError(t, server.EncryptSSEResponse(resp, 1<<16))
	assert.Equal(t, ContentType, resp.Header.Get("Content-Type"))
	assert.NotEmpty(t, resp.Header.Get(ResponseOAEHeader))

	httpResp := &http.Response{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: resp.Body}
	ds, err := client.DecryptResponse(httpResp, testMaxFrameSize)
	require.NoError(t, err)

	assert.Equal(t, "text/event-stream", ds.Header.Get("Content-Type"))
	assert.Empty(t, ds.Header.Values(ResponseOAEHeader))
	assert.Equal(t, "keepme", ds.Header.Get("Custom"))

	plaintext, err := io.ReadAll(ds.Body)
	require.NoError(t, err)
	require.NoError(t, ds.Body.Close())
	assert.Equal(t, sseStream, string(plaintext))
}

func TestSingleUse(t *testing.T) {
	cases := map[string]func(t *testing.T) error{
		"client EncryptRequest": func(t *testing.T) error {
			client, err := InitializeClientExchange(testSecret())
			require.NoError(t, err)
			require.NoError(t, client.EncryptRequest(newPlainRequest(t, "application/json", []byte("first"))))
			return client.EncryptRequest(newPlainRequest(t, "application/json", []byte("second")))
		},
		"client DecryptResponse": func(t *testing.T) error {
			client, server := handshake(t, testSecret())
			unary := &forwarder.UnaryResponse{StatusCode: http.StatusOK, Header: http.Header{}, Body: []byte("body")}
			require.NoError(t, server.EncryptUnaryResponse(unary))
			httpResp := &http.Response{
				StatusCode: unary.StatusCode,
				Header:     unary.Header.Clone(),
				Body:       io.NopCloser(bytes.NewReader(unary.Body)),
			}
			_, err := client.DecryptResponse(httpResp, testMaxFrameSize)
			require.NoError(t, err)

			second := &http.Response{Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(nil))}
			second.Header.Set("Content-Type", ContentType)
			_, err = client.DecryptResponse(second, testMaxFrameSize)
			return err
		},
		"server DecryptRequest": func(t *testing.T) error {
			secret := testSecret()
			client, err := InitializeClientExchange(secret)
			require.NoError(t, err)
			req := newPlainRequest(t, "application/json", []byte("payload"))
			require.NoError(t, client.EncryptRequest(req))

			server, err := InitializeServerExchange(secret, req)
			require.NoError(t, err)
			require.NoError(t, server.DecryptRequest(req))
			return server.DecryptRequest(req)
		},
	}

	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, run(t), ErrAlreadyStarted)
		})
	}
}

func TestServerResponseEncryptionSingleUse(t *testing.T) {
	unary := func(s *ServerExchange) error {
		return s.EncryptUnaryResponse(&forwarder.UnaryResponse{Header: http.Header{}, Body: []byte("body")})
	}
	sse := func(s *ServerExchange) error {
		return s.EncryptSSEResponse(&forwarder.StreamingResponse{
			Header: http.Header{},
			Body:   io.NopCloser(strings.NewReader("data: x\n\n")),
		}, 1<<16)
	}

	cases := map[string]struct {
		first, second func(*ServerExchange) error
	}{
		"unary then unary": {first: unary, second: unary},
		"sse then sse":     {first: sse, second: sse},
		"unary then sse":   {first: unary, second: sse},
		"sse then unary":   {first: sse, second: unary},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, server := handshake(t, testSecret())
			require.NoError(t, tc.first(server))
			require.ErrorIs(t, tc.second(server), ErrAlreadyStarted)
		})
	}
}

func TestInitializeServerExchangeRejectsPlain(t *testing.T) {
	req := newPlainRequest(t, "application/json", []byte("payload"))
	_, err := InitializeServerExchange(testSecret(), req)
	require.ErrorIs(t, err, ErrNotEncrypted)
}

func TestDecryptResponseRejectsPlain(t *testing.T) {
	client, err := InitializeClientExchange(testSecret())
	require.NoError(t, err)

	resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(nil))}
	resp.Header.Set("Content-Type", "application/json")

	_, err = client.DecryptResponse(resp, testMaxFrameSize)
	require.ErrorIs(t, err, ErrNotEncrypted)
}
