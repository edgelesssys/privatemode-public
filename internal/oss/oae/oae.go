// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package oae provides Online Authenticated Encryption (streaming of individually encrypted
// messages) for request/response exchanges between a requester and a responder.
//
// An exchange consists of two independently keyed, ordered streams. The request stream contains
// messages from requester to responder and the response stream works in the opposite direction.
// Messages in each stream are individually sealed and opened, and are authenticated against their
// position in the stream so that reordering, truncation or duplication are detected.
//
// For the full construction see RFC-29 "Online Authenticated Encryption Module".
package oae

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
)

// Implementation notes:
//   - [cipher.AEAD] instances are not auto-cleared on stream end (could be added)
//   - There is also no Discard() API to clear [PendingOpener] secrets and [cipher.AEAD] instances
//     (aligns with stdlib crypto conventions).
//   - [runtime.SetFinalizer] for zeroing sensitive buffers (notably the PRK in [PendingOpener])
//     could be implemented later.
//   - Evaluate [runtime/secret.Do] once stable for zeroing "temporary" memory in functions
//     performing cryptographic/security-relevant operations.

// Sentinel errors returned by this package. Stream scoped errors are phrased as stream states so
// they read well when re-emitted by other methods once the stream has reached terminal state.
var (
	// Stream processing.

	// ErrStreamClosed is returned when the stream has properly ended via a final message.
	ErrStreamClosed = errors.New("oae: stream closed")
	// ErrStreamTruncated is returned when a stream is signalled finished by the user but no final
	// message has been opened.
	ErrStreamTruncated = errors.New("oae: stream truncated")
	// ErrMessageLimitReached is returned when a stream has reached the per-stream message limit
	// of 2^32 and cannot progress further.
	ErrMessageLimitReached = errors.New("oae: stream message limit reached")
	// ErrMalformedSealedMessage is returned when a sealed message does not have the expected
	// structure.
	ErrMalformedSealedMessage = errors.New("oae: stream malformed sealed message")
	// ErrMessageAuthFailed is returned when AEAD authentication of a sealed message fails.
	ErrMessageAuthFailed = errors.New("oae: stream message authentication failed")

	// Initialization and headers.

	// ErrMalformedHeader is returned when a request or response header does not have the expected
	// structure.
	ErrMalformedHeader = errors.New("oae: malformed header")
	// ErrInvalidSharedSecret is returned when the shared secret does not have the expected length.
	ErrInvalidSharedSecret = errors.New("oae: invalid shared secret")
	// ErrResponseAlreadyAccepted is returned by [PendingOpener.Accept] if it has already been
	// called before.
	ErrResponseAlreadyAccepted = errors.New("oae: response already accepted")
)

const (
	sharedSecretSize = 32
	saltSize         = 32
	streamKeySize    = 32

	gcmNonceSize = 12
	gcmTagSize   = 16

	// aadSize is the size in bytes of the AAD. It has a fixed layout of 4-byte big-endian uint32
	// sequence number followed by a single final-flag byte.
	aadSize = 4 + 1

	// maxMessages is the per-stream message limit to keep nonce collision probability low.
	maxMessages uint64 = 1 << 32

	requestKeyInfo        = "privatemode oae request key\x00"
	responseKeyInfoPrefix = "privatemode oae response key\x00"
)

const (
	// RequestHeaderSize is the size in bytes of a request header.
	RequestHeaderSize = saltSize

	// ResponseHeaderSize is the size in bytes of a response header.
	ResponseHeaderSize = saltSize

	// SealedOverheadSize is size in bytes of the overhead a sealed message has over the plaintext
	// message. The overhead is the final-flag byte and the nonce and tag for AEAD. An empty
	// plaintext produces exactly this size.
	SealedOverheadSize = 1 + gcmNonceSize + gcmTagSize
)

// Sealer encrypts messages on one direction of an exchange.
// It is not safe for concurrent use.
type Sealer struct {
	aead   cipher.AEAD
	seqCtr uint64
	// err, once non-nil, captures the stream's terminal state. For a clean close, that
	// is [ErrStreamClosed].
	err error
	// aadBuf is a reusable buffer for per-message AAD. Its capacity is aadSize.
	aadBuf []byte
}

// SealMessage encrypts plaintext as the next message in the stream, appending the sealed message
// to dst and returning the resulting slice.
// If final is true, the message is authenticated as the terminal message and further calls return
// [ErrStreamClosed].
func (s *Sealer) SealMessage(dst, plaintext []byte, final bool) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.seqCtr >= maxMessages {
		s.err = ErrMessageLimitReached
		return nil, s.err
	}

	finalFlagByte := finalFlagToByte(final)
	aad := appendAAD(s.aadBuf[:0], uint32(s.seqCtr), finalFlagByte)
	defer zeroize(aad)

	dst = append(dst, finalFlagByte)
	// [cipher.NewGCMWithRandomNonce] generates and prepends the 12-byte random nonce itself.
	dst = s.aead.Seal(dst, nil, plaintext, aad)

	s.seqCtr++
	if final {
		s.err = ErrStreamClosed
	}
	return dst, nil
}

// Opener decrypts messages on one direction of an exchange. It is not
// safe for concurrent use.
type Opener struct {
	aead   cipher.AEAD
	seqCtr uint64
	// err, once non-nil, captures the stream's terminal state. For a clean close, that
	// is [ErrStreamClosed].
	err error
	// aadBuf is a reusable buffer for per-message AAD. Its capacity is aadSize.
	aadBuf []byte
}

// OpenMessage decrypts sealed as the next message in the stream, appending the plaintext to dst
// and returning the resulting slice. dst may be written to even if an error is returned.
// After a successful open of a final message further calls return [ErrStreamClosed].
func (o *Opener) OpenMessage(dst, sealed []byte) ([]byte, error) {
	if o.err != nil {
		return nil, o.err
	}
	if o.seqCtr >= maxMessages {
		o.err = ErrMessageLimitReached
		return nil, o.err
	}
	if len(sealed) < SealedOverheadSize {
		o.err = ErrMalformedSealedMessage
		return nil, o.err
	}

	finalFlagByte := sealed[0]
	finalFlag, err := finalFlagFromByte(finalFlagByte)
	if err != nil {
		o.err = ErrMalformedSealedMessage
		return nil, o.err
	}

	aad := appendAAD(o.aadBuf[:0], uint32(o.seqCtr), finalFlagByte)
	defer zeroize(aad)
	// [cipher.NewGCMWithRandomNonce] extracts 12-byte nonce itself.
	plaintext, err := o.aead.Open(dst, nil, sealed[1:], aad)
	if err != nil {
		o.err = ErrMessageAuthFailed
		return nil, o.err
	}

	o.seqCtr++
	if finalFlag {
		o.err = ErrStreamClosed
	}
	return plaintext, nil
}

// Finish must be called to signal that the stream has ended and reports whether a truncation
// occurred. It returns nil only if the stream has completed cleanly with a final message and
// [ErrStreamTruncated] if no final has been received. Finish is idempotent.
func (o *Opener) Finish() error {
	switch {
	case o.err == nil:
		// Not closed, so truncation detected
		o.err = ErrStreamTruncated
		return o.err
	case errors.Is(o.err, ErrStreamTruncated):
		// Truncation error return is idempotent
		return o.err
	case errors.Is(o.err, ErrStreamClosed):
		// Clean closure, nil return is idempotent
		return nil
	default:
		// Wrap Open()-related errors for clearer message.
		return fmt.Errorf("oae: finish on failed stream: %w", o.err)
	}
}

// PendingOpener is a single-use handle held by the requester between request and response
// initialization. It is not safe for concurrent use.
type PendingOpener struct {
	// exchangePRK is generated by [InitializeRequester] and required to derive the response stream
	// key. It is consumed on [PendingOpener.Accept] and set to nil immediately.
	exchangePRK []byte
}

// InitializeRequester initializes an exchange as the requester. It returns the sealer for the
// request stream, a pending opener for the response stream and the oae request header.
// The header must be transported to the responder to be able to open the request stream.
func InitializeRequester(secret []byte) (*Sealer, *PendingOpener, []byte, error) {
	// Shared secret length is checked for exact equality, not a minimum. The real minimum is also
	// entropy, which cannot be checked for.
	if len(secret) != sharedSecretSize {
		return nil, nil, nil, ErrInvalidSharedSecret
	}

	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, nil, fmt.Errorf("oae: generate request salt: %w", err)
	}

	exchangePRK, err := deriveExchangePRK(secret, salt)
	if err != nil {
		return nil, nil, nil, err
	}
	defer zeroize(exchangePRK)

	requestKey, err := deriveRequestKey(exchangePRK)
	if err != nil {
		return nil, nil, nil, err
	}
	defer zeroize(requestKey)

	sealer, err := newSealer(requestKey)
	if err != nil {
		return nil, nil, nil, err
	}

	pending := &PendingOpener{
		// Clone here, to be able to use defer zeroize() above
		exchangePRK: bytes.Clone(exchangePRK),
	}
	return sealer, pending, salt, nil
}

// Accept returns the response stream opener derived from the oae response header received from the
// responder. Accept is strictly single-use. The exchange PRK is consumed on entry, so any further
// call returns [ErrResponseAlreadyAccepted].
func (p *PendingOpener) Accept(responseHeader []byte) (*Opener, error) {
	// Consuming PRK on entry is stricter than specified by RFC-29.
	if p.exchangePRK == nil {
		return nil, ErrResponseAlreadyAccepted
	}

	prk := p.exchangePRK
	p.exchangePRK = nil
	defer zeroize(prk)

	if len(responseHeader) != ResponseHeaderSize {
		return nil, ErrMalformedHeader
	}

	responseKey, err := deriveResponseKey(prk, responseHeader)
	if err != nil {
		return nil, err
	}
	defer zeroize(responseKey)

	return newOpener(responseKey)
}

// InitializeResponder initializes an exchange as the responder. It returns the opener for the
// request stream, a sealer for the response stream and the oae response header.
// The header must be transported to the requester to be able to open the response stream.
func InitializeResponder(secret, requestHeader []byte) (*Opener, *Sealer, []byte, error) {
	if len(secret) != sharedSecretSize {
		return nil, nil, nil, ErrInvalidSharedSecret
	}
	if len(requestHeader) != RequestHeaderSize {
		return nil, nil, nil, ErrMalformedHeader
	}

	exchangePRK, err := deriveExchangePRK(secret, requestHeader)
	if err != nil {
		return nil, nil, nil, err
	}
	defer zeroize(exchangePRK)

	requestKey, err := deriveRequestKey(exchangePRK)
	if err != nil {
		return nil, nil, nil, err
	}
	defer zeroize(requestKey)

	responseSalt := make([]byte, saltSize)
	if _, err := rand.Read(responseSalt); err != nil {
		return nil, nil, nil, fmt.Errorf("oae: generate response salt: %w", err)
	}

	responseKey, err := deriveResponseKey(exchangePRK, responseSalt)
	if err != nil {
		return nil, nil, nil, err
	}
	defer zeroize(responseKey)

	opener, err := newOpener(requestKey)
	if err != nil {
		return nil, nil, nil, err
	}
	sealer, err := newSealer(responseKey)
	if err != nil {
		return nil, nil, nil, err
	}

	return opener, sealer, responseSalt, nil
}

func deriveExchangePRK(secret, requestSalt []byte) ([]byte, error) {
	prk, err := hkdf.Extract(sha256.New, secret, requestSalt)
	if err != nil {
		return nil, fmt.Errorf("oae: HKDF-Extract exchange PRK: %w", err)
	}
	return prk, nil
}

func deriveRequestKey(exchangePRK []byte) ([]byte, error) {
	key, err := hkdf.Expand(sha256.New, exchangePRK, requestKeyInfo, streamKeySize)
	if err != nil {
		return nil, fmt.Errorf("oae: HKDF-Expand request key: %w", err)
	}
	return key, nil
}

func deriveResponseKey(exchangePRK, responseSalt []byte) ([]byte, error) {
	info := responseKeyInfoPrefix + string(responseSalt)
	key, err := hkdf.Expand(sha256.New, exchangePRK, info, streamKeySize)
	if err != nil {
		return nil, fmt.Errorf("oae: HKDF-Expand response key: %w", err)
	}
	return key, nil
}

func newSealer(key []byte) (*Sealer, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return &Sealer{
		aead:   aead,
		aadBuf: make([]byte, aadSize),
	}, nil
}

func newOpener(key []byte) (*Opener, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return &Opener{
		aead:   aead,
		aadBuf: make([]byte, aadSize),
	}, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("oae: AES cipher: %w", err)
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, fmt.Errorf("oae: AES-GCM: %w", err)
	}
	return aead, nil
}

// appendAAD appends the AAD's fixed layout to dst, see [aadSize].
func appendAAD(dst []byte, seq uint32, finalFlag byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, seq)
	return append(dst, finalFlag)
}

func finalFlagToByte(final bool) byte {
	if final {
		return 0x01
	}
	return 0x00
}

func finalFlagFromByte(val byte) (bool, error) {
	switch val {
	case 0x00:
		return false, nil
	case 0x01:
		return true, nil
	default:
		return false, ErrMalformedSealedMessage
	}
}

// zeroize overwrites the passed b fully with zero bytes. It should be used to clear memory
// containing security-relevant data (FIPS calls these Sensitive Security Parameters) after use.
func zeroize(b []byte) {
	clear(b)
	// Hint to the compiler to keep b alive until this point, so it doesn't elide the clear() call.
	runtime.KeepAlive(b)
}
