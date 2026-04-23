// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package privatemode

import (
	"errors"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/crypto"
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

func TestTryDecryptResponseError(t *testing.T) {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i)
	}
	secretID := "test-secret"

	testCases := map[string]struct {
		makeError    func(t *testing.T, cipher *crypto.RequestCipher) error
		skipFields   forwarder.FieldSelector
		wantDecr     bool
		wantContains string
	}{
		"non-ResponseError passes through": {
			makeError: func(*testing.T, *crypto.RequestCipher) error {
				return errors.New("some other error")
			},
			wantContains: "some other error",
		},
		"unencrypted ResponseError passes through": {
			makeError: func(*testing.T, *crypto.RequestCipher) error {
				return &ResponseError{
					StatusCode: 400,
					Body:       []byte(`{"error":{"message":"plain error"}}`),
				}
			},
			wantContains: "plain error",
		},
		"encrypted ResponseError is decrypted": {
			makeError: func(t *testing.T, clientCipher *crypto.RequestCipher) error {
				// Encrypt a dummy request field to set the nonce
				// (mirrors what prepareChatCompletionsRequest does).
				encReq, err := clientCipher.Encrypt("test")
				require.NoError(t, err)

				// Extract the nonce so we can simulate server-side encryption.
				nonce, err := crypto.GetNonceFromCipher(encReq)
				require.NoError(t, err)

				// Server encrypts the error body with response seqNum 0.
				errorJSON := `{"message":"context length exceeded","type":"invalid_request_error"}`
				encrypted, err := crypto.EncryptMessage(errorJSON, secret, secretID, nonce, 0)
				require.NoError(t, err)

				body := []byte(`{"error":` + encrypted + `}`)
				return &ResponseError{StatusCode: 400, Body: body}
			},
			skipFields:   forwarder.FieldSelector{{"id"}, {"usage"}},
			wantDecr:     true,
			wantContains: "context length exceeded",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			cipher, err := crypto.NewRequestCipher(secret, secretID)
			require.NoError(t, err)

			origErr := tc.makeError(t, cipher)
			result := tryDecryptResponseError(origErr, cipher, tc.skipFields)

			if !tc.wantDecr {
				assert.Equal(t, origErr, result)
				assert.ErrorContains(t, result, tc.wantContains)
				return
			}

			var respErr *ResponseError
			require.ErrorAs(t, result, &respErr)
			assert.Equal(t, 400, respErr.StatusCode)
			assert.Contains(t, string(respErr.Body), tc.wantContains)
			assert.NotContains(t, string(respErr.Body), secretID)
		})
	}
}
