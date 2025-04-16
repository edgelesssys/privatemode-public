// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// package crypto provides an API for encrypting inference requests and responses using AES-GCM.
// Each message is encrypted using a unique IV and a secret key, called the inference secret.
// The inference secret is associated with an inference secret ID. The ID is included in the encrypted message
// as a hint for the decrypting party to select the correct decryption key.
// Encrypted messages are in the format '"id:nonce:iv:ciphertext"', where
//   - 'id' is the inference secret ID,
//   - 'nonce' is a hex-encoded nonce that is equal for all messages in a single request/response exchange but unique for each exchange,
//   - 'iv' is the unique, hex-encoded IV,
//   - and 'ciphertext' is the hex-encoded cipher text from encrypting a plain input.
//
// Double quotes are included to ensure the value is a valid JSON string.
// This is required since our mutation functions (callers of this package) don't make assumptions about the format of the input or output,
// neither are they able to perform any type distinctions.
// They only take a raw JSON value, hand it of to a mutation function (functions from this package) and write back the result into the JSON structure.
// No type marshalling is performed at any point.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// RequestCipher provides encryption for all messages of a single request and decryption for its response messages.
// You can't reuse the object to encrypt another request. You must create a new one for a new request.
type RequestCipher struct {
	secret []byte
	id     string

	nonce     []byte // nonce that is included in all encrypted messages and authenticated on decryption
	encSeqNum uint32 // sequence number for encrypting messages
	decSeqNum uint32 // sequence number for decrypting messages
}

// NewRequestCipher creates a new RequestCipher.
func NewRequestCipher(inferenceSecret []byte, inferenceSecretID string) (*RequestCipher, error) {
	nonce, err := GenerateNonce()
	if err != nil {
		return nil, err
	}
	return &RequestCipher{
		secret:    inferenceSecret,
		id:        inferenceSecretID,
		nonce:     nonce,
		encSeqNum: 0,
		decSeqNum: 0,
	}, nil
}

// Encrypt encrypts a message of the request.
// The function returns the encrypted message in the format '"id:nonce:iv:ciphertext"'.
// Be careful with using the output as values in Go structs, as the output already contains double quotes.
// This may lead to issues when the output is marshalled as a string.
func (c *RequestCipher) Encrypt(plaintext string) (string, error) {
	if c.decSeqNum != 0 {
		return "", errors.New("can't encrypt another request after decrypting a response")
	}
	ciphertext, err := EncryptMessage(plaintext, c.secret, c.id, c.nonce, c.encSeqNum)
	if err != nil {
		return "", err
	}
	c.encSeqNum++
	return ciphertext, nil
}

// DecryptResponse decrypts a response.
func (c *RequestCipher) DecryptResponse(ciphertext string) (string, error) {
	plaintext, err := DecryptMessage(ciphertext, c.secret, c.nonce, c.decSeqNum)
	if err != nil {
		return "", err
	}
	c.decSeqNum++
	return plaintext, nil
}

// EncryptMessage encrypts a message using the given inference secret and associates the ciphertext with the given inference secret id.
// The function returns the encrypted message in the format '"id:nonce:iv:ciphertext"'.
// Most users should use RequestCipher instead of this lower-level function.
func EncryptMessage(plainText string, inferenceSecret []byte, inferenceSecretID string, nonce []byte, sequenceNumber uint32) (string, error) {
	sealer, err := getSealer(inferenceSecret)
	if err != nil {
		return "", err
	}
	iv, err := GenerateNonce()
	if err != nil {
		return "", err
	}
	cipherText := sealer.Seal(nil, iv, []byte(plainText), makeAdditionalData(nonce, sequenceNumber))
	return fmt.Sprintf(
		"\"%s:%s:%s:%s\"", // Add double quotes to make the returned value a valid JSON string
		inferenceSecretID,
		hex.EncodeToString(nonce),
		hex.EncodeToString(iv),
		hex.EncodeToString(cipherText),
	), nil
}

// DecryptMessage decrypts a message using the given inference secret.
// The message is expected to be in the format '"id:nonce:iv:ciphertext"'.
func DecryptMessage(cipherText string, inferenceSecret []byte, nonce []byte, sequenceNumber uint32) (string, error) {
	cipherText = strings.Trim(cipherText, `"`)
	parts := strings.Split(cipherText, ":")
	if len(parts) != 4 {
		return "", fmt.Errorf("invalid message format: expected format '\"id:nonce:iv:cipher\"'")
	}

	iv, err := hex.DecodeString(parts[2])
	if err != nil {
		return "", err
	}
	cipher, err := hex.DecodeString(parts[3])
	if err != nil {
		return "", err
	}

	sealer, err := getSealer(inferenceSecret)
	if err != nil {
		return "", err
	}

	plainText, err := sealer.Open(nil, iv, cipher, makeAdditionalData(nonce, sequenceNumber))
	return string(plainText), err
}

// GetIDFromCipher returns the inference secret ID from the given cipher text.
func GetIDFromCipher(cipherText string) (string, error) {
	cipherText = strings.Trim(cipherText, `"`)
	id, _, found := strings.Cut(cipherText, ":")
	if !found {
		return "", fmt.Errorf("invalid message format: expected format '\"id:nonce:iv:cipher\"'")
	}
	return id, nil
}

// GetNonceFromCipher returns the nonce from the given cipher text.
func GetNonceFromCipher(cipherText string) ([]byte, error) {
	cipherText = strings.Trim(cipherText, `"`)
	parts := strings.Split(cipherText, ":")
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid message format: expected format '\"id:nonce:iv:cipher\"'")
	}
	return hex.DecodeString(parts[1])
}

// GenerateNonce creates a nonce for a request.
func GenerateNonce() ([]byte, error) {
	nonce := make([]byte, 12)
	_, err := io.ReadFull(rand.Reader, nonce)
	return nonce, err
}

func getSealer(inferenceSecret []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(inferenceSecret)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// makeAdditionalData creates the additional authenticated data for replay protection.
func makeAdditionalData(nonce []byte, sequenceNumber uint32) []byte {
	data := append([]byte(nil), nonce...)
	return binary.LittleEndian.AppendUint32(data, sequenceNumber)
}
