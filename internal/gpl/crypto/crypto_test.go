// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package crypto

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecrypt(t *testing.T) {
	inferenceSecret := bytes.Repeat([]byte{0x42}, 32)
	otherInferenceSecret := bytes.Repeat([]byte{0x43}, 32)

	nonce, err := GenerateNonce()
	require.NoError(t, err)
	otherNonce, err := GenerateNonce()
	require.NoError(t, err)

	testCases := map[string]struct {
		encSecret []byte
		encNonce  []byte
		encSeqNum uint32

		decSecret []byte
		decNonce  []byte
		decSeqNum uint32

		wantEncErr bool
		wantDecErr bool
	}{
		"valid": {
			encSecret: inferenceSecret,
			encNonce:  nonce,
			encSeqNum: 2,
		},
		"invalid key size": {
			encSecret:  bytes.Repeat([]byte{0x42}, 30),
			encNonce:   nonce,
			encSeqNum:  2,
			wantEncErr: true,
		},
		"invalid decryption secret": {
			encSecret:  inferenceSecret,
			encNonce:   nonce,
			encSeqNum:  2,
			decSecret:  otherInferenceSecret,
			wantDecErr: true,
		},
		"invalid decryption nonce": {
			encSecret:  inferenceSecret,
			encNonce:   nonce,
			encSeqNum:  2,
			decNonce:   otherNonce,
			wantDecErr: true,
		},
		"invalid decryption sequence number": {
			encSecret:  inferenceSecret,
			encNonce:   nonce,
			encSeqNum:  2,
			decSeqNum:  3,
			wantDecErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			// use same values for decryption as for encryption if not set
			if tc.decSecret == nil {
				tc.decSecret = tc.encSecret
			}
			if tc.decNonce == nil {
				tc.decNonce = tc.encNonce
			}
			if tc.decSeqNum == 0 {
				tc.decSeqNum = tc.encSeqNum
			}

			const message = "message"
			const id = "id"

			cipherText, err := EncryptMessage(message, tc.encSecret, id, tc.encNonce, tc.encSeqNum)
			if tc.wantEncErr {
				assert.Error(err)
				return
			}
			require.NoError(err)

			// Check that quotes are added
			assert.Equal(`"`, string(cipherText[0]))
			assert.Equal(`"`, string(cipherText[len(cipherText)-1]))

			// Check ID
			cipherTextTrimmed := strings.Trim(cipherText, `"`)
			associatedID, _, _ := strings.Cut(cipherTextTrimmed, ":")
			assert.Equal(id, associatedID)
			encodedID, err := GetIDFromCipher(cipherText)
			assert.NoError(err)
			assert.Equal(id, encodedID)

			plaintext, err := DecryptMessage(cipherText, tc.decSecret, tc.decNonce, tc.decSeqNum)
			if tc.wantDecErr {
				assert.Error(err)
				return
			}
			require.NoError(err)

			assert.Equal(message, plaintext)
		})
	}
}

func TestRequestCipher(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	secret := bytes.Repeat([]byte{2}, 16)
	rc, err := NewRequestCipher(secret, "id")
	require.NoError(err)

	// client: use RequestCipher to encrypt messages
	ct1, err := rc.Encrypt("req1")
	require.NoError(err)
	ct2, err := rc.Encrypt("req2")
	require.NoError(err)

	// server: get nonce from first message
	nonce, err := GetNonceFromCipher(ct1)
	require.NoError(err)

	// server: decrypt request messages
	pt, err := DecryptMessage(ct1, secret, nonce, 0)
	require.NoError(err)
	assert.Equal("req1", pt)
	pt, err = DecryptMessage(ct2, secret, nonce, 1)
	require.NoError(err)
	assert.Equal("req2", pt)

	// server: encrypt response messages
	resp1, err := EncryptMessage("resp1", secret, "id", nonce, 0)
	require.NoError(err)
	resp2, err := EncryptMessage("resp2", secret, "id", nonce, 1)
	require.NoError(err)

	// server: encrypt another response message with wrong nonce
	wrongNonce, err := GenerateNonce()
	require.NoError(err)
	resp3WithWrongNonce, err := EncryptMessage("resp3", secret, "id", wrongNonce, 2)
	require.NoError(err)

	// client: decrypt response messages
	pt, err = rc.DecryptResponse(resp1)
	require.NoError(err)
	assert.Equal("resp1", pt)
	pt, err = rc.DecryptResponse(resp2)
	require.NoError(err)
	assert.Equal("resp2", pt)

	// client: decrypting response message with wrong nonce should fail
	_, err = rc.DecryptResponse(resp3WithWrongNonce)
	assert.Error(err)

	// client: decrypting response message in wrong sequence should fail
	_, err = rc.DecryptResponse(resp2)
	assert.Error(err)

	// client: encrypting another request message should fail
	_, err = rc.Encrypt("foo")
	assert.Error(err)
}
