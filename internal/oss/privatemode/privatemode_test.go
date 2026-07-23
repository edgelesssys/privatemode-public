// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package privatemode

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/edgelesssys/continuum/internal/oss/encbody"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseError(t *testing.T) {
	err := &ResponseError{StatusCode: 400, Body: []byte(`{"error":"bad request"}`)}
	assert.Equal(t, `unexpected status code 400: {"error":"bad request"}`, err.Error())

	var target *ResponseError
	assert.ErrorAs(t, err, &target)
}

func TestNewResponseError(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"error":"plain error"}`))),
	}
	got, err := newResponseError(resp)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, got.StatusCode)
	assert.JSONEq(t, `{"error":"plain error"}`, string(got.Body))
	assert.Equal(t, "application/json", got.Header.Get("Content-Type"))
}

func TestDecryptResponseErrorBody(t *testing.T) {
	secret := bytes.Repeat([]byte{0x42}, 32)
	const plainErrorJSON = `{"message":"context length exceeded","type":"invalid_request_error"}`

	encryptBody := func(t *testing.T, ce *encbody.ClientExchange, plain []byte) (http.Header, []byte) {
		t.Helper()
		req := newTestRequest(t)
		require.NoError(t, ce.EncryptRequest(req))

		se, err := encbody.InitializeServerExchange(secret, req)
		require.NoError(t, err)
		require.NoError(t, se.DecryptRequest(req))

		resp := &forwarder.UnaryResponse{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       plain,
		}
		require.NoError(t, se.EncryptUnaryResponse(resp))
		return resp.Header, resp.Body
	}

	testCases := map[string]struct {
		makeRespErr func(t *testing.T, ce *encbody.ClientExchange) *ResponseError
		wantErr     bool
		wantBody    string
	}{
		"plain body is returned as-is": {
			makeRespErr: func(*testing.T, *encbody.ClientExchange) *ResponseError {
				return &ResponseError{
					StatusCode: http.StatusBadRequest,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       []byte(`{"error":"plain error"}`),
				}
			},
			wantBody: `{"error":"plain error"}`,
		},
		"encrypted body is decrypted": {
			makeRespErr: func(t *testing.T, ce *encbody.ClientExchange) *ResponseError {
				header, body := encryptBody(t, ce, []byte(plainErrorJSON))
				return &ResponseError{StatusCode: http.StatusBadRequest, Header: header, Body: body}
			},
			wantBody: plainErrorJSON,
		},
		"encrypted body with missing OAE header fails": {
			makeRespErr: func(*testing.T, *encbody.ClientExchange) *ResponseError {
				header := http.Header{"Content-Type": []string{encbody.ContentType}}
				return &ResponseError{StatusCode: http.StatusBadRequest, Header: header, Body: []byte("garbage")}
			},
			wantErr: true,
		},
		"tampered encrypted body fails": {
			makeRespErr: func(t *testing.T, ce *encbody.ClientExchange) *ResponseError {
				header, body := encryptBody(t, ce, []byte(plainErrorJSON))
				body[len(body)-1] ^= 1 // flip a bit in the auth tag
				return &ResponseError{StatusCode: http.StatusBadRequest, Header: header, Body: body}
			},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ce, err := encbody.InitializeClientExchange(secret)
			require.NoError(t, err)

			respErr := tc.makeRespErr(t, ce)
			decrypted, decryptErr := decryptResponseErrorBody(respErr, ce)

			if tc.wantErr {
				require.Error(t, decryptErr)
				assert.Nil(t, decrypted)
				return
			}

			require.NoError(t, decryptErr)
			require.NotNil(t, decrypted)
			assert.Equal(t, http.StatusBadRequest, decrypted.StatusCode)
			assert.Equal(t, tc.wantBody, string(decrypted.Body))
		})
	}
}

func TestListModelsWithOptionsPreservesHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "custom-value", req.Header.Get("X-Custom-Header"))
		assert.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
		w.Header().Set("X-Request-ID", "req_models")
		_, err := w.Write([]byte(`{"data":[],"object":"list"}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	client := New(auth.APIKey("test-key")).
		WithAPIBaseURL(server.URL).
		WithHTTPClient(server.Client())
	resp, err := client.ListModelsWithOptions(t.Context(), RequestOptions{
		Header: http.Header{
			"Authorization":   []string{"Bearer must-be-overridden"},
			"X-Custom-Header": []string{"custom-value"},
		},
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"data":[],"object":"list"}`, string(resp.Body))
	assert.Equal(t, "req_models", resp.Header.Get("X-Request-ID"))
}

func newTestRequest(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/", io.NopCloser(bytes.NewReader([]byte("req"))))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	return req
}
