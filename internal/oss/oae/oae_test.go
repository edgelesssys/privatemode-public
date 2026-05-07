// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package oae

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// streamPair pairs a Sealer with the Opener on the same stream. I.e. the Opener decrypts what the
// Sealer encrypts.
type streamPair struct {
	sealer *Sealer
	opener *Opener
}

// exchange holds both directions of an initialized exchange.
type exchange struct {
	request  streamPair
	response streamPair
}

func testSecret() []byte {
	return bytes.Repeat([]byte{0x42}, sharedSecretSize)
}

// newExchange runs the full requester/responder initialization with [testSecret] and returns
// both oriented stream pairs.
func newExchange(t testing.TB) exchange {
	t.Helper()
	secret := testSecret()

	reqSealer, pending, reqHeader, err := InitializeRequester(secret)
	require.NoError(t, err)
	reqOpener, respSealer, respHeader, err := InitializeResponder(secret, reqHeader)
	require.NoError(t, err)
	respOpener, err := pending.Accept(respHeader)
	require.NoError(t, err)
	return exchange{
		request:  streamPair{reqSealer, reqOpener},
		response: streamPair{respSealer, respOpener},
	}
}

func mustSeal(t testing.TB, s *Sealer, plaintext []byte, final bool) []byte {
	t.Helper()
	sealed, err := s.SealMessage(nil, plaintext, final)
	require.NoError(t, err)
	return sealed
}

// flipBit returns a copy of b with a single bit flipped.
func flipBit(b []byte, byteIdx, bitIdx int) []byte {
	out := bytes.Clone(b)
	out[byteIdx] ^= 1 << bitIdx
	return out
}

func TestRoundTrip(t *testing.T) {
	sse := []byte("event: message\ndata: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n")
	big := bytes.Repeat([]byte{'x'}, (1<<20)+1)

	tests := map[string][][]byte{
		"empty single":                          {{}},
		"single byte single":                    {{'x'}},
		"SSE event single":                      {sse},
		"large payload single":                  {big},
		"multiple non-final plus empty final":   {[]byte("m1"), []byte("m2"), []byte("m3"), {}},
		"multiple non-final plus payload final": {[]byte("m1"), []byte("m2"), []byte("m3"), []byte("final")},
	}

	directions := map[string]func(exchange) streamPair{
		"request":  func(e exchange) streamPair { return e.request },
		"response": func(e exchange) streamPair { return e.response },
	}

	for name, plaintexts := range tests {
		for dirName, pick := range directions {
			t.Run(name+"/"+dirName, func(t *testing.T) {
				assert := assert.New(t)
				require := require.New(t)

				stream := pick(newExchange(t))
				for i, pt := range plaintexts {
					final := i == len(plaintexts)-1
					sealed, err := stream.sealer.SealMessage(nil, pt, final)
					require.NoError(err)
					assert.Len(sealed, len(pt)+SealedOverheadSize)

					got, err := stream.opener.OpenMessage(nil, sealed)
					require.NoError(err)
					assert.Equal(string(pt), string(got))
				}
				assert.NoError(stream.opener.Finish())
			})
		}
	}
}

func TestOpenFailsBitflip(t *testing.T) {
	plaintext := []byte("hello, world")
	ciphertextStart := 1 + gcmNonceSize
	tagStart := ciphertextStart + len(plaintext)

	// Sealed message layout: flag(1) || nonce(12) || ciphertext(N) || tag(16).

	tests := map[string]struct {
		byteIdx int
		bitIdx  int
		wantErr error
	}{
		"final flag toggles to other valid value": {0, 0, ErrMessageAuthFailed},
		"final flag toggles to invalid value":     {0, 1, ErrMalformedSealedMessage},
		"nonce":                                   {4, 0, ErrMessageAuthFailed},
		"ciphertext":                              {ciphertextStart + 2, 0, ErrMessageAuthFailed},
		"tag":                                     {tagStart + 5, 0, ErrMessageAuthFailed},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			e := newExchange(t)
			sealed := mustSeal(t, e.request.sealer, plaintext, false)
			_, err := e.request.opener.OpenMessage(nil, flipBit(sealed, tc.byteIdx, tc.bitIdx))
			assert.ErrorIs(t, err, tc.wantErr)
			// Terminal error is sticky and must be returned again
			sealed = mustSeal(t, e.request.sealer, plaintext, false)
			_, err = e.request.opener.OpenMessage(nil, sealed)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestOpenFailsReplayReorderDrop(t *testing.T) {
	t.Run("replay", func(t *testing.T) {
		e := newExchange(t)
		m := mustSeal(t, e.request.sealer, []byte("m"), false)
		_, err := e.request.opener.OpenMessage(nil, m)
		require.NoError(t, err)
		_, err = e.request.opener.OpenMessage(nil, m)
		assert.ErrorIs(t, err, ErrMessageAuthFailed)
	})

	t.Run("reorder and drop", func(t *testing.T) {
		// Reorder and Drop are both out-of-order delivery
		e := newExchange(t)
		_ = mustSeal(t, e.request.sealer, []byte("m1"), false)
		m2 := mustSeal(t, e.request.sealer, []byte("m2"), false)
		_, err := e.request.opener.OpenMessage(nil, m2)
		assert.ErrorIs(t, err, ErrMessageAuthFailed)
	})
}

func TestOpenFailsCrossStream(t *testing.T) {
	e := newExchange(t)

	t.Run("open request message on response stream", func(t *testing.T) {
		fromRequest := mustSeal(t, e.request.sealer, []byte("req"), false)
		_, err := e.response.opener.OpenMessage(nil, fromRequest)
		assert.ErrorIs(t, err, ErrMessageAuthFailed)
	})

	t.Run("open response message on request stream", func(t *testing.T) {
		fromResponse := mustSeal(t, e.response.sealer, []byte("resp"), false)
		_, err := e.request.opener.OpenMessage(nil, fromResponse)
		assert.ErrorIs(t, err, ErrMessageAuthFailed)
	})
}

func TestOpenFailsCrossExchange(t *testing.T) {
	e1 := newExchange(t)
	e2 := newExchange(t)

	sealed := mustSeal(t, e1.request.sealer, []byte("from e1"), false)
	_, err := e2.request.opener.OpenMessage(nil, sealed)
	assert.ErrorIs(t, err, ErrMessageAuthFailed)
}

func TestHeaderFailsBitflip(t *testing.T) {
	secret := testSecret()

	t.Run("request header", func(t *testing.T) {
		reqSealer, _, reqHeader, err := InitializeRequester(secret)
		require.NoError(t, err)
		reqOpener, _, _, err := InitializeResponder(secret, flipBit(reqHeader, 0, 0))
		require.NoError(t, err)

		sealed := mustSeal(t, reqSealer, []byte("hi"), false)
		_, err = reqOpener.OpenMessage(nil, sealed)
		assert.ErrorIs(t, err, ErrMessageAuthFailed)
	})

	t.Run("response header", func(t *testing.T) {
		_, pending, reqHeader, err := InitializeRequester(secret)
		require.NoError(t, err)
		_, respSealer, respHeader, err := InitializeResponder(secret, reqHeader)
		require.NoError(t, err)
		respOpener, err := pending.Accept(flipBit(respHeader, 0, 0))
		require.NoError(t, err)

		sealed := mustSeal(t, respSealer, []byte("hi"), false)
		_, err = respOpener.OpenMessage(nil, sealed)
		assert.ErrorIs(t, err, ErrMessageAuthFailed)
	})
}

func TestValidation(t *testing.T) {
	validSecret := testSecret()
	shortSecret := validSecret[1:]
	validReqHeader := bytes.Repeat([]byte{0x00}, RequestHeaderSize)
	shortReqHeader := bytes.Repeat([]byte{0x00}, RequestHeaderSize-1)
	shortRespHeader := bytes.Repeat([]byte{0x00}, ResponseHeaderSize-1)

	t.Run("InitializeRequester short secret", func(t *testing.T) {
		_, _, _, err := InitializeRequester(shortSecret)
		assert.ErrorIs(t, err, ErrInvalidSharedSecret)
	})
	t.Run("InitializeResponder short secret", func(t *testing.T) {
		_, _, _, err := InitializeResponder(shortSecret, validReqHeader)
		assert.ErrorIs(t, err, ErrInvalidSharedSecret)
	})
	t.Run("InitializeResponder short header", func(t *testing.T) {
		_, _, _, err := InitializeResponder(validSecret, shortReqHeader)
		assert.ErrorIs(t, err, ErrMalformedHeader)
	})
	t.Run("Accept short header", func(t *testing.T) {
		_, pending, _, err := InitializeRequester(validSecret)
		require.NoError(t, err)
		_, err = pending.Accept(shortRespHeader)
		assert.ErrorIs(t, err, ErrMalformedHeader)
	})
	t.Run("OpenMessage short sealed", func(t *testing.T) {
		e := newExchange(t)
		tooShort := bytes.Repeat([]byte{0}, SealedOverheadSize-1)
		_, err := e.request.opener.OpenMessage(nil, tooShort)
		assert.ErrorIs(t, err, ErrMalformedSealedMessage)
		// Terminal error is sticky and must be returned again
		sealed := mustSeal(t, e.request.sealer, []byte{}, false)
		_, err = e.request.opener.OpenMessage(nil, sealed)
		assert.ErrorIs(t, err, ErrMalformedSealedMessage)
	})
}

func TestDstAppend(t *testing.T) {
	e := newExchange(t)
	plaintext := []byte("payload")
	prefix := []byte("prefix:")

	sealed := bytes.Clone(prefix)
	sealed, err := e.request.sealer.SealMessage(sealed, plaintext, true)
	require.NoError(t, err)
	assert.Equal(t, sealed[:len(prefix)], prefix)

	opened := bytes.Clone(prefix)
	opened, err = e.request.opener.OpenMessage(opened, sealed[len(prefix):])
	require.NoError(t, err)
	assert.Equal(t, opened[:len(prefix)], prefix)
	assert.Equal(t, opened[len(prefix):], plaintext)
}

func TestSealerClosedAfterFinal(t *testing.T) {
	e := newExchange(t)
	_, err := e.request.sealer.SealMessage(nil, []byte("final"), true)
	require.NoError(t, err)

	_, err = e.request.sealer.SealMessage(nil, []byte("again"), false)
	assert.ErrorIs(t, err, ErrStreamClosed)
	// Terminal error is sticky and must be returned again
	_, err = e.request.sealer.SealMessage(nil, []byte("again"), true)
	assert.ErrorIs(t, err, ErrStreamClosed)
}

func TestOpenerClosedAfterFinal(t *testing.T) {
	e := newExchange(t)
	sealed := mustSeal(t, e.request.sealer, []byte("final"), true)
	_, err := e.request.opener.OpenMessage(nil, sealed)
	require.NoError(t, err)

	// Terminal error is sticky and must be returned again
	_, err = e.request.opener.OpenMessage(nil, sealed)
	assert.ErrorIs(t, err, ErrStreamClosed)
}

func TestFinish(t *testing.T) {
	t.Run("truncated: no message opened", func(t *testing.T) {
		e := newExchange(t)
		assert.ErrorIs(t, e.request.opener.Finish(), ErrStreamTruncated)
		assert.ErrorIs(t, e.request.opener.Finish(), ErrStreamTruncated)
	})

	t.Run("truncated: no final seen", func(t *testing.T) {
		e := newExchange(t)
		sealed := mustSeal(t, e.request.sealer, []byte("m"), false)
		_, err := e.request.opener.OpenMessage(nil, sealed)
		require.NoError(t, err)
		assert.ErrorIs(t, e.request.opener.Finish(), ErrStreamTruncated)
		assert.ErrorIs(t, e.request.opener.Finish(), ErrStreamTruncated)
	})

	t.Run("clean close", func(t *testing.T) {
		e := newExchange(t)
		sealed := mustSeal(t, e.request.sealer, []byte("final"), true)
		_, err := e.request.opener.OpenMessage(nil, sealed)
		require.NoError(t, err)
		assert.NoError(t, e.request.opener.Finish())
		assert.NoError(t, e.request.opener.Finish())
	})

	t.Run("after malformed", func(t *testing.T) {
		e := newExchange(t)
		tooShort := bytes.Repeat([]byte{0}, SealedOverheadSize-1)
		_, err := e.request.opener.OpenMessage(nil, tooShort)
		require.ErrorIs(t, err, ErrMalformedSealedMessage)

		// Terminal error from OpenMessage() is sticky and must be returned by Finish()
		assert.ErrorIs(t, e.request.opener.Finish(), ErrMalformedSealedMessage)
		assert.ErrorIs(t, e.request.opener.Finish(), ErrMalformedSealedMessage)
	})

	t.Run("after auth failure", func(t *testing.T) {
		e := newExchange(t)
		sealed := mustSeal(t, e.request.sealer, []byte("hi"), false)
		_, err := e.request.opener.OpenMessage(nil, flipBit(sealed, len(sealed)-1, 0))
		require.ErrorIs(t, err, ErrMessageAuthFailed)

		// Terminal error from OpenMessage() is sticky and must be returned by Finish()
		assert.ErrorIs(t, e.request.opener.Finish(), ErrMessageAuthFailed)
		assert.ErrorIs(t, e.request.opener.Finish(), ErrMessageAuthFailed)
	})
}

func TestAcceptSingleUse(t *testing.T) {
	secret := testSecret()
	validRespHeader := bytes.Repeat([]byte{0x00}, ResponseHeaderSize)
	shortRespHeader := bytes.Repeat([]byte{0x00}, ResponseHeaderSize-1)

	t.Run("after successful Accept", func(t *testing.T) {
		_, pending, reqHeader, err := InitializeRequester(secret)
		require.NoError(t, err)
		_, _, respHeader, err := InitializeResponder(secret, reqHeader)
		require.NoError(t, err)

		_, err = pending.Accept(respHeader)
		require.NoError(t, err)
		_, err = pending.Accept(respHeader)
		assert.ErrorIs(t, err, ErrResponseAlreadyAccepted)
	})

	t.Run("after failed Accept", func(t *testing.T) {
		_, pending, _, err := InitializeRequester(secret)
		require.NoError(t, err)

		_, err = pending.Accept(shortRespHeader)
		require.ErrorIs(t, err, ErrMalformedHeader)
		_, err = pending.Accept(validRespHeader)
		assert.ErrorIs(t, err, ErrResponseAlreadyAccepted)
	})
}

func TestSealerMessageLimit(t *testing.T) {
	e := newExchange(t)
	// Internal modification of [Sealer] state
	e.request.sealer.seqCtr = maxMessages
	_, err := e.request.sealer.SealMessage(nil, []byte("x"), false)
	assert.ErrorIs(t, err, ErrMessageLimitReached)
	// Terminal error is sticky and must be returned again
	_, err = e.request.sealer.SealMessage(nil, []byte("x"), false)
	assert.ErrorIs(t, err, ErrMessageLimitReached)
}

func TestOpenerMessageLimit(t *testing.T) {
	e := newExchange(t)
	sealed := mustSeal(t, e.request.sealer, []byte("x"), false)
	// Internal modification of [Opener] state
	e.request.opener.seqCtr = maxMessages
	_, err := e.request.opener.OpenMessage(nil, sealed)
	assert.ErrorIs(t, err, ErrMessageLimitReached)
	// Terminal error is sticky and must be returned again
	_, err = e.request.opener.OpenMessage(nil, sealed)
	assert.ErrorIs(t, err, ErrMessageLimitReached)
}

var benchPlaintext = []byte("event: message\ndata: {\"choices\":[{\"delta\":{\"content\":\"The quick brown fox jumps over the lazy dog\"}}]}\n\n")

func BenchmarkSealMessage(b *testing.B) {
	b.ReportAllocs()
	e := newExchange(b)
	dst := make([]byte, 0, SealedOverheadSize+len(benchPlaintext))
	for b.Loop() {
		_, err := e.request.sealer.SealMessage(dst[:0], benchPlaintext, false)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOpenMessage(b *testing.B) {
	b.ReportAllocs()
	e := newExchange(b)

	sealed := make([][]byte, b.N)
	for i := range sealed {
		s, err := e.request.sealer.SealMessage(nil, benchPlaintext, false)
		if err != nil {
			b.Fatal(err)
		}
		sealed[i] = s
	}

	dst := make([]byte, 0, len(benchPlaintext))
	b.ResetTimer()
	for i := range b.N {
		_, err := e.request.opener.OpenMessage(dst[:0], sealed[i])
		if err != nil {
			b.Fatal(err)
		}
	}
}
