package cipher

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/edgelesssys/continuum/inference-proxy/internal/secrets"
	crypto "github.com/edgelesssys/continuum/internal/oss/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptResponse(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	message := "message"
	id := "id"
	nonce := []byte("nonce")
	secret := bytes.Repeat([]byte{0x42}, 32)
	secrets := secrets.New(stubSecretGetter{}, nil)
	cipher := New(secrets)

	// No secrets set
	_, err := cipher.encryptResponse(t.Context(), id, message, nonce, 2)
	assert.Error(err)

	// Add a secret
	secrets.Set(id, secret)

	// Encrypt
	cipherText, err := cipher.encryptResponse(t.Context(), id, message, nonce, 2)
	require.NoError(err)

	// Encrypt with wrong id
	_, err = cipher.encryptResponse(t.Context(), "wrong", message, nonce, 2)
	assert.Error(err)

	// Decrypt
	plainText, err := crypto.DecryptMessage(cipherText, secret, nonce, 2)
	require.NoError(err)
	assert.Equal(message, plainText)
}

func TestDecryptRequest(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	message := "message"
	id := "id"
	secret := bytes.Repeat([]byte{0x42}, 32)
	secrets := secrets.New(stubSecretGetter{}, nil)
	cipher := New(secrets)

	// Prepare ciphertext
	requestCipher, err := crypto.NewRequestCipher(secret, id)
	require.NoError(err)
	cipherText, err := requestCipher.Encrypt(message)
	require.NoError(err)

	nonce, err := cipher.getNonce(cipherText)
	require.NoError(err)

	// No secrets set
	_, _, err = cipher.decryptRequest(t.Context(), cipherText, nonce, 0)
	assert.Error(err)

	// Add a secret
	secrets.Set(id, secret)

	// Decrypt
	plainText, gotID, err := cipher.decryptRequest(t.Context(), cipherText, nonce, 0)
	require.NoError(err)
	assert.Equal(message, plainText)
	assert.Equal(id, gotID)

	// Try to decrypt message with wrong format
	_, _, err = cipher.decryptRequest(t.Context(), "wrong", nonce, 0)
	assert.Error(err)
}

func TestResponseCipherDecryptRequest(t *testing.T) {
	testCases := map[string]struct {
		responseCipher   *responseCipher
		requestBody      string
		expectedResponse string
		wantErr          bool
	}{
		"decrypt message": {
			responseCipher: &responseCipher{
				cipher: &stubCipher{
					decipherMsg: "plainText",
				},
			},
			requestBody:      "encryptedText",
			expectedResponse: "plainText",
		},
		"get nonce error": {
			responseCipher: &responseCipher{
				cipher: &stubCipher{
					decipherMsg: "plainText",
					getNonceErr: assert.AnError,
				},
			},
			requestBody: "encryptedText",
			wantErr:     true,
		},
		"decryption error": {
			responseCipher: &responseCipher{
				cipher: &stubCipher{
					decipherErr: assert.AnError,
				},
			},
			requestBody: "encryptedText",
			wantErr:     true,
		},
		"decrypt after encrypting a response": {
			responseCipher: &responseCipher{
				cipher: &stubCipher{
					decipherMsg: "plainText",
				},
				encSeqNum: 1,
			},
			requestBody: "encryptedText",
			wantErr:     true,
		},
		"decrypt message with different ID": {
			responseCipher: &responseCipher{
				cipher: &stubCipher{
					decipherMsg: "plainText",
				},
				id: "some-id",
			},
			requestBody: "encryptedText",
			wantErr:     true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			result, err := tc.responseCipher.DecryptRequest(t.Context())(tc.requestBody)

			if tc.wantErr {
				assert.Error(err)
				return
			}

			require.NoError(err)
			assert.Equal(tc.expectedResponse, result)
		})
	}
}

func TestResponseCipherEncryptResponse(t *testing.T) {
	testCases := map[string]struct {
		responseCipher   *responseCipher
		responseBody     string
		expectedResponse string
		wantErr          bool
	}{
		"encrypt message": {
			responseCipher: &responseCipher{
				cipher: &stubCipher{
					cipherMsg: "encryptedText",
				},
				id:        "unit-test",
				nonce:     []byte("nonce"),
				decSeqNum: 1,
			},
			responseBody:     "plainText",
			expectedResponse: "encryptedText",
		},
		"encrypt error": {
			responseCipher: &responseCipher{
				cipher: &stubCipher{
					cipherErr: assert.AnError,
				},
				id:        "unit-test",
				nonce:     []byte("nonce"),
				decSeqNum: 1,
			},
			responseBody: "plainText",
			wantErr:      true,
		},
		"encrypt without ID set": {
			responseCipher: &responseCipher{
				cipher: &stubCipher{
					cipherMsg: "encryptedText",
				},
				id:        "",
				nonce:     []byte("nonce"),
				decSeqNum: 1,
			},
			responseBody: "plainText",
			wantErr:      true,
		},
		"encrypt without first decrypting a message": {
			responseCipher: &responseCipher{
				cipher: &stubCipher{
					cipherMsg: "encryptedText",
				},
				id:        "unit-test",
				nonce:     []byte("nonce"),
				decSeqNum: 0,
			},
			responseBody: "plainText",
			wantErr:      true,
		},
		"encrypt without nonce set": {
			responseCipher: &responseCipher{
				cipher: &stubCipher{
					cipherMsg: "encryptedText",
				},
				id:        "unit-test",
				nonce:     nil,
				decSeqNum: 1,
			},
			responseBody: "plainText",
			wantErr:      true,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			result, err := tc.responseCipher.EncryptResponse(t.Context())(tc.responseBody)
			if tc.wantErr {
				assert.Error(err)
				return
			}

			assert.NoError(err)
			assert.Equal(tc.expectedResponse, result)
		})
	}
}

func TestResponseCipherEncryptDecrypt(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	message := "message"
	id := "id"
	secret := bytes.Repeat([]byte{0x42}, 32)
	secrets := secrets.New(stubSecretGetter{}, nil)
	cipher := New(secrets)

	// Add a secret
	secrets.Set(id, secret)

	// Create a responseCipher
	responseCipher := cipher.NewResponseCipher()

	// Prepare ciphertexts for the ResponseCipher to decrypt
	requestCipher, err := crypto.NewRequestCipher(secret, id)
	require.NoError(err)

	cipherText1, err := requestCipher.Encrypt(message)
	require.NoError(err)

	cipherText2, err := requestCipher.Encrypt(message)
	require.NoError(err)

	// Decrypt the messages
	plainText1, err := responseCipher.DecryptRequest(t.Context())(cipherText1)
	assert.NoError(err)
	assert.Equal(message, plainText1)

	plainText2, err := responseCipher.DecryptRequest(t.Context())(cipherText2)
	assert.NoError(err)
	assert.Equal(message, plainText2)

	// Encrypt a response
	responseBody := "response"
	encryptedResponse1, err := responseCipher.EncryptResponse(t.Context())(responseBody)
	assert.NoError(err)

	encryptedResponse2, err := responseCipher.EncryptResponse(t.Context())(responseBody)
	assert.NoError(err)

	// Decrypt the response
	plainResponse1, err := requestCipher.DecryptResponse(encryptedResponse1)
	require.NoError(err)
	assert.Equal(responseBody, plainResponse1)

	plainResponse2, err := requestCipher.DecryptResponse(encryptedResponse2)
	require.NoError(err)
	assert.Equal(responseBody, plainResponse2)
}

type stubCipher struct {
	cipherErr   error
	cipherMsg   string
	decipherErr error
	decipherMsg string
	getNonceErr error
}

func (s *stubCipher) encryptResponse(_ context.Context, _, _ string, _ []byte, _ uint32) (string, error) {
	return s.cipherMsg, s.cipherErr
}

func (s *stubCipher) decryptRequest(_ context.Context, _ string, _ []byte, _ uint32) (text, id string, err error) {
	return s.decipherMsg, "unit-test", s.decipherErr
}

func (s *stubCipher) getNonce(string) ([]byte, error) {
	return []byte("nonce"), s.getNonceErr
}

type stubSecretGetter struct{}

func (s stubSecretGetter) GetSecret(_ context.Context, _ string) ([]byte, error) {
	return nil, errors.New("not found")
}
