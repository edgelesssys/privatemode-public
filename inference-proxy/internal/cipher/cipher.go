// package cipher handles the server-side decryption of sensitive data in inference requests and encryption of responses using AES-GCM.
package cipher

import (
	"errors"
	"fmt"

	"github.com/edgelesssys/continuum/inference-proxy/internal/secrets"
	crypto "github.com/edgelesssys/continuum/internal/gpl/crypto"
)

// Cipher encrypts and decrypts messages.
type Cipher struct {
	inferenceSecrets *secrets.Secrets
}

// New creates a new Cipher.
func New(secrets *secrets.Secrets) *Cipher {
	return &Cipher{
		inferenceSecrets: secrets,
	}
}

// encryptResponse encrypts a message.
// The message is encrypted using the secret associated with the given id.
// The function returns the encrypted message in the format 'id:nonce:iv:cipher'.
func (c *Cipher) encryptResponse(id, message string, requestNonce []byte, sequenceNumber uint32) (string, error) {
	secret, ok := c.inferenceSecrets.Get(id)
	if !ok {
		return "", fmt.Errorf("no secret for ID %q", id)
	}
	return crypto.EncryptMessage(message, secret, id, requestNonce, sequenceNumber)
}

// decryptRequest decrypts a message.
// The message is expected to be in the format '"id:nonce:iv:cipher"'.
// On success, the function returns the plain text and the id.
func (c *Cipher) decryptRequest(message string, nonce []byte, sequenceNumber uint32) (text, id string, err error) {
	id, err = crypto.GetIDFromCipher(message)
	if err != nil {
		return "", "", err
	}
	secret, ok := c.inferenceSecrets.Get(id)
	if !ok {
		return "", "", fmt.Errorf("no secret for ID %q", id)
	}
	text, err = crypto.DecryptMessage(message, secret, nonce, sequenceNumber)
	return text, id, err
}

// getNonce returns the nonce from the given cipher text.
func (*Cipher) getNonce(ciphertext string) ([]byte, error) {
	return crypto.GetNonceFromCipher(ciphertext)
}

// ResponseCipher handles encryption and decryption of one request-response exchange.
// It acts as the server component for [crypto.RequestCipher].
type ResponseCipher struct {
	cipher cipher

	id        string
	nonce     []byte
	encSeqNum uint32 // sequence number for encrypting messages
	decSeqNum uint32 // sequence number for decrypting messages
}

// NewResponseCipher creates a new [ResponseCipher].
func (c *Cipher) NewResponseCipher() *ResponseCipher {
	return &ResponseCipher{
		cipher:    c,
		id:        "",
		nonce:     nil,
		encSeqNum: 0,
		decSeqNum: 0,
	}
}

// DecryptRequest decrypts data sent by a client.
func (c *ResponseCipher) DecryptRequest(encryptedData string) (res string, err error) {
	if c.encSeqNum != 0 {
		return "", errors.New("can't decrypt another request after encrypting a response")
	}

	// get request nonce from first message
	if c.decSeqNum == 0 {
		c.nonce, err = c.cipher.getNonce(encryptedData)
		if err != nil {
			return "", fmt.Errorf("getting nonce: %w", err)
		}
	}

	plainData, fieldID, err := c.cipher.decryptRequest(encryptedData, c.nonce, c.decSeqNum)
	if err != nil {
		return "", fmt.Errorf("deciphering input: %w", err)
	}
	c.decSeqNum++

	if c.id == "" {
		c.id = fieldID
	}
	// All fields must be encrypted with the same ID
	if c.id != fieldID {
		return "", fmt.Errorf("deciphering input: multiple different IDs used for encrypting data: %q does not match %q", c.id, fieldID)
	}

	return plainData, nil
}

// EncryptResponse encrypts data to send back to a client.
// It may only be called after first decrypting data using [ResponseCipher.DecryptRequest].
func (c *ResponseCipher) EncryptResponse(plainData string) (string, error) {
	if len(c.nonce) == 0 || c.decSeqNum == 0 || c.id == "" {
		return "", errors.New("can't encrypt response without first decrypting a request")
	}

	encryptedData, err := c.cipher.encryptResponse(c.id, plainData, c.nonce, c.encSeqNum)
	if err != nil {
		return "", fmt.Errorf("enciphering response: %w", err)
	}
	c.encSeqNum++

	return encryptedData, nil
}

// cipher is the interface for encryption and decryption of messages read and send by the proxy.
type cipher interface {
	encryptResponse(id, message string, requestNonce []byte, sequenceNumber uint32) (string, error)
	decryptRequest(message string, nonce []byte, sequenceNumber uint32) (text, id string, err error)
	getNonce(ciphertext string) ([]byte, error)
}
