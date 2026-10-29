// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package privatemode

import (
	"bytes"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/encbody"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/ocspheader"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
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
		require.NoError(t, ce.EncryptRequest(req, nil))

		se, err := encbody.InitializeServerExchange(secret, req)
		require.NoError(t, err)
		_, err = se.DecryptRequest(req)
		require.NoError(t, err)

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

func TestRetrieveModelWithOptions(t *testing.T) {
	const model = "org/model?#%"
	const body = `{"id":"org/model?#%","object":"model"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "/v1/models/org%2Fmodel%3F%23%25", req.URL.RequestURI())
		assert.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
		assert.Equal(t, "custom-value", req.Header.Get("X-Custom-Header"))
		w.Header().Set("X-Request-ID", "req_model")
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	client := New(auth.APIKey("test-key")).
		WithAPIBaseURL(server.URL).
		WithHTTPClient(server.Client())
	resp, err := client.RetrieveModelWithOptions(t.Context(), model, RequestOptions{
		Header: http.Header{"X-Custom-Header": []string{"custom-value"}},
	})
	require.NoError(t, err)
	assert.JSONEq(t, body, string(resp.Body))
	assert.Equal(t, "req_model", resp.Header.Get("X-Request-ID"))
}

func TestClientOCSPPolicy(t *testing.T) {
	secret := secretmanager.Secret{ID: "test", Data: bytes.Repeat([]byte{1}, 32)}
	policies := make(chan *ocspheader.Header, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchange, err := encbody.InitializeServerExchange(secret.Data, r)
		if !assert.NoError(t, err) {
			return
		}
		headers, err := exchange.DecryptRequest(r)
		if !assert.NoError(t, err) {
			return
		}
		policy, err := ocspheader.Unmarshal(headers[constants.PrivatemodeNvidiaOCSPPolicyHeader])
		if !assert.NoError(t, err) {
			return
		}
		policies <- policy
		resp := &forwarder.UnaryResponse{Header: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(`{}`)}
		if !assert.NoError(t, exchange.EncryptUnaryResponse(resp)) {
			return
		}
		maps.Copy(w.Header(), resp.Header)
		_, err = w.Write(resp.Body)
		assert.NoError(t, err)
	}))
	defer backend.Close()

	for name, tc := range map[string]struct {
		policy   NvidiaOCSPPolicy
		statuses []ocspheader.AllowStatus
		wantErr  bool
	}{
		"default": {statuses: []ocspheader.AllowStatus{ocspheader.AllowStatusGood}},
		"unknown without revoked": {
			policy:   NvidiaOCSPPolicy{AllowUnknown: true},
			statuses: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown},
		},
		"fractional grace period": {
			policy:   NvidiaOCSPPolicy{RevokedGracePeriod: 90 * time.Minute},
			statuses: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusRevoked},
		},
		"custom policy": {
			policy: NvidiaOCSPPolicy{AllowUnknown: true, RevokedGracePeriod: 48 * time.Hour},
			statuses: []ocspheader.AllowStatus{
				ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown, ocspheader.AllowStatusRevoked,
			},
		},
		"negative grace period": {policy: NvidiaOCSPPolicy{RevokedGracePeriod: -time.Hour}, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			client := New(auth.APIKey("test")).WithAPIBaseURL(backend.URL).WithHTTPClient(backend.Client())
			client.currentSecret = secret
			client.WithNvidiaOCSPPolicy(tc.policy)
			var previousCutoff time.Time
			for request := range 2 {
				if request > 0 && tc.policy.RevokedGracePeriod > 0 {
					// The wire header has second precision. Cross a full second to
					// distinguish a rolling cutoff from one fixed at initialization.
					time.Sleep(1100 * time.Millisecond)
				}
				before := time.Now().Add(-tc.policy.RevokedGracePeriod).Truncate(time.Second)
				_, err := client.EmbeddingsWithOptions(t.Context(), []byte(`{"model":"test","input":"hello"}`), RequestOptions{})
				if tc.wantErr {
					require.ErrorContains(t, err, "grace period must be nonnegative")
					return
				}
				require.NoError(t, err)
				select {
				case policy := <-policies:
					assert.ElementsMatch(t, tc.statuses, policy.AllowedStatuses)
					assert.False(t, policy.RevokedNbf.Before(before))
					assert.False(t, policy.RevokedNbf.After(time.Now().Add(-tc.policy.RevokedGracePeriod)))
					if request > 0 && tc.policy.RevokedGracePeriod > 0 {
						assert.True(t, policy.RevokedNbf.After(previousCutoff), "the cutoff must advance between requests")
					}
					previousCutoff = policy.RevokedNbf
				default:
					t.Fatal("backend did not receive an authenticated policy")
				}
			}
		})
	}
}

func newTestRequest(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/", io.NopCloser(bytes.NewReader([]byte("req"))))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	return req
}
