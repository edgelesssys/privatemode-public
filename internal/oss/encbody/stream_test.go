// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package encbody

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/oae"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test-local mirrors of the production caps callers pass into the package. Defined here so the
// package's tests don't need to repeat the constants.* qualifier at every call site.
const (
	testMaxFrameSize     uint32 = constants.MaxEncryptedFrameSize
	testMaxSSEEventBytes        = constants.MaxSSEEventBytes
)

// testFrameReader constructs a streamReader with only framing fields set, for tests that
// exercise the wire format without OAE.
func testFrameReader(r io.Reader, maxFrameSize uint32) *streamReader {
	return &streamReader{r: bufio.NewReader(r), maxFrameSize: maxFrameSize}
}

// testSecret returns always the same slice usable as an OAE secret.
func testSecret() []byte {
	s := make([]byte, 32)
	for i := range s {
		s[i] = byte(i + 1)
	}
	return s
}

// oaeStream pairs a sealer with the opener that decrypts what it seals.
type oaeStream struct {
	sealer *oae.Sealer
	opener *oae.Opener
}

// newOAEExchange runs a full OAE requester/responder handshake and returns the request and response
// streams.
func newOAEExchange(t testing.TB) (request, response oaeStream) {
	t.Helper()
	secret := testSecret()
	reqSealer, pending, reqHeader, err := oae.InitializeRequester(secret)
	require.NoError(t, err)
	reqOpener, respSealer, respHeader, err := oae.InitializeResponder(secret, reqHeader)
	require.NoError(t, err)
	respOpener, err := pending.Accept(respHeader)
	require.NoError(t, err)
	return oaeStream{reqSealer, reqOpener}, oaeStream{respSealer, respOpener}
}

// streamFrame describes one frame to seal into an encrypted stream via buildEncryptedStream.
type streamFrame struct {
	payload []byte
	final   bool
}

// buildEncryptedStream seals the given frames in order with sealer and returns the encrypted bytes.
func buildEncryptedStream(t testing.TB, sealer *oae.Sealer, frames ...streamFrame) []byte {
	t.Helper()
	var buf []byte
	for i, f := range frames {
		var err error
		buf, err = appendEncryptedFrame(buf, sealer, f.payload, f.final)
		require.NoError(t, err, "frame %d", i)
	}
	return buf
}

func TestEncryptDecryptStream(t *testing.T) {
	tests := map[string]struct {
		frames       []streamFrame
		wantMsgs     [][]byte
		wantFinalErr error
		modifyStream func([]byte) []byte
	}{
		"multiple messages": {
			frames: []streamFrame{
				{[]byte("hello"), false},
				{[]byte("world"), false},
				{[]byte("!"), true},
			},
			wantMsgs:     [][]byte{[]byte("hello"), []byte("world"), []byte("!")},
			wantFinalErr: io.EOF,
		},
		"single message": {
			frames:       []streamFrame{{[]byte("hello"), true}},
			wantMsgs:     [][]byte{[]byte("hello")},
			wantFinalErr: io.EOF,
		},
		"empty plaintext": {
			frames:       []streamFrame{{nil, true}},
			wantMsgs:     [][]byte{nil},
			wantFinalErr: io.EOF,
		},
		"empty stream": {
			frames:       nil,
			wantMsgs:     nil,
			wantFinalErr: oae.ErrStreamTruncated,
		},
		"truncated missing final": {
			frames: []streamFrame{
				{[]byte("m1"), false},
				{[]byte("m2"), false},
				{[]byte("m3"), false},
			},
			wantMsgs:     [][]byte{[]byte("m1"), []byte("m2"), []byte("m3")},
			wantFinalErr: oae.ErrStreamTruncated,
		},
		"mid-frame cut": {
			frames: []streamFrame{
				{[]byte("frame1"), false},
				{[]byte("body that gets cut"), true},
			},
			modifyStream: func(b []byte) []byte { return b[:len(b)-1] },
			wantMsgs:     [][]byte{[]byte("frame1")},
			wantFinalErr: io.ErrUnexpectedEOF,
		},
		"corrupted oae payload": {
			frames: []streamFrame{
				{[]byte("frame1"), false},
				{[]byte("frame2"), true},
			},
			modifyStream: func(b []byte) []byte {
				// Flip a single bit inside the second frame's encrypted payload.
				out := bytes.Clone(b)
				out[len(out)-5] ^= 1
				return out
			},
			wantMsgs:     [][]byte{[]byte("frame1")},
			wantFinalErr: oae.ErrMessageAuthFailed,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			request, _ := newOAEExchange(t)
			buf := buildEncryptedStream(t, request.sealer, tc.frames...)
			if tc.modifyStream != nil {
				buf = tc.modifyStream(buf)
			}

			sr := newStreamReader(bytes.NewReader(buf), request.opener, testMaxFrameSize)

			for _, want := range tc.wantMsgs {
				got, err := sr.appendDecryptedFrame(nil)
				require.NoError(t, err)
				assert.Equal(t, want, got)
			}

			_, err := sr.appendDecryptedFrame(nil)
			assert.ErrorIs(t, err, tc.wantFinalErr)
		})
	}
}

func appendEncodedFrame(dst []byte, payload []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(payload)))
	dst = append(dst, payload...)
	return dst
}

func TestStreamReaderAppendFramePayload(t *testing.T) {
	tests := map[string]struct {
		frames    [][]byte
		dstPrefix string
	}{
		"empty frame":    {frames: [][]byte{{}}},
		"tiny frame":     {frames: [][]byte{{'x'}}},
		"large frame":    {frames: [][]byte{bytes.Repeat([]byte{'x'}, (1<<20)+7)}},
		"many frames":    {frames: [][]byte{[]byte("a"), []byte("bb"), []byte("ccc"), nil, []byte("ddddd")}},
		"appends to dst": {frames: [][]byte{[]byte("payload")}, dstPrefix: "prefix:"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var buf []byte
			for _, f := range tc.frames {
				buf = appendEncodedFrame(buf, f)
			}

			fr := testFrameReader(bytes.NewReader(buf), 1<<24)
			for i, want := range tc.frames {
				dst := []byte(tc.dstPrefix)
				got, err := fr.appendFramePayload(dst)
				require.NoError(t, err, "frame %d", i)
				assert.Equal(t, tc.dstPrefix+string(want), string(got), "frame %d", i)
			}
			_, err := fr.appendFramePayload(nil)
			assert.ErrorIs(t, err, io.EOF)
		})
	}
}

func TestStreamReaderAppendFramePayloadErrors(t *testing.T) {
	tests := map[string]struct {
		buf      []byte
		maxFrame uint32
		dst      []byte
		wantErr  error
	}{
		"oversized frame": {
			buf:      appendEncodedFrame(nil, bytes.Repeat([]byte{'x'}, 100)),
			maxFrame: 50,
			dst:      []byte("keep"),
			wantErr:  errFrameTooLarge,
		},
		"truncated length prefix": {
			buf:     []byte{0x00, 0x00, 0x00},
			wantErr: io.ErrUnexpectedEOF,
		},
		"truncated payload": {
			// Length prefix announces 10 bytes, only 4 delivered.
			buf:     []byte{0x00, 0x00, 0x00, 0x0a, 'a', 'b', 'c', 'd'},
			wantErr: io.ErrUnexpectedEOF,
		},
		"announced length delivers nothing": {
			buf:     binary.BigEndian.AppendUint32(nil, 64*1024),
			wantErr: io.ErrUnexpectedEOF,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			maxFrameSize := tc.maxFrame
			if maxFrameSize == 0 {
				maxFrameSize = testMaxFrameSize
			}
			fr := testFrameReader(bytes.NewReader(tc.buf), maxFrameSize)
			got, err := fr.appendFramePayload(tc.dst)
			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, string(tc.dst), string(got), "dst keeps its original contents on error")
		})
	}
}
