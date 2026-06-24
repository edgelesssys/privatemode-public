// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package encbody

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/edgelesssys/continuum/internal/oss/oae"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decryptAll(t *testing.T, encrypted []byte, opener *oae.Opener) (appHeader, []byte) {
	t.Helper()
	d, h, err := openDecrypter(io.NopCloser(bytes.NewReader(encrypted)), opener, testMaxFrameSize)
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()
	plain, err := io.ReadAll(d)
	require.NoError(t, err)
	return h, plain
}

func TestEncryptDecryptWhole(t *testing.T) {
	tests := map[string][]byte{
		"empty": nil,
		"small": []byte(`{"hello":"world"}`),
		"large": bytes.Repeat([]byte{'x'}, 5*1024*1024),
	}

	for name, plaintext := range tests {
		t.Run(name, func(t *testing.T) {
			request, _ := newOAEExchange(t)
			hdr := appHeader{ContentType: "application/json"}

			enc, err := encryptWhole(plaintext, request.sealer, hdr)
			require.NoError(t, err)

			gotHdr, got := decryptAll(t, enc, request.opener)
			assert.Equal(t, hdr, gotHdr)
			assert.Equal(t, string(plaintext), string(got))
		})
	}
}

func TestSSEEncrypter(t *testing.T) {
	tests := map[string]string{
		"single event":    "data: hello\n\n",
		"multiple events": "data: a\n\ndata: b\n\ndata: [DONE]\n\n",
		"empty stream":    "",
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			request, _ := newOAEExchange(t)
			hdr := appHeader{ContentType: "text/event-stream"}

			enc, err := openSSEEncrypter(io.NopCloser(strings.NewReader(body)), request.sealer, hdr, testMaxSSEEventBytes)
			require.NoError(t, err)
			encrypted, err := io.ReadAll(enc)
			require.NoError(t, err)
			require.NoError(t, enc.Close())

			gotHdr, got := decryptAll(t, encrypted, request.opener)
			assert.Equal(t, hdr, gotHdr)
			assert.Equal(t, body, string(got))
		})
	}
}

func TestSSEEncrypterPlaintextErrors(t *testing.T) {
	plaintextErr := errors.New("plaintext read failed")
	tests := map[string]struct {
		plaintext io.Reader
		wantErr   error
	}{
		"plaintext read failed": {
			plaintext: &errAfterReader{
				data: []byte("data: a\n\ndata: b\n\n"),
				err:  plaintextErr,
			},
			wantErr: plaintextErr,
		},
		"event too long": {
			plaintext: strings.NewReader(strings.Repeat("x", testMaxSSEEventBytes+1)),
			wantErr:   bufio.ErrTooLong,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, response := newOAEExchange(t)

			enc, err := openSSEEncrypter(io.NopCloser(tc.plaintext), response.sealer, appHeader{}, testMaxSSEEventBytes)
			require.NoError(t, err)
			_, err = io.ReadAll(enc)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestDecrypterLeftoverHandling(t *testing.T) {
	request, _ := newOAEExchange(t)
	plaintext := []byte("the quick brown fox jumps over the lazy dog")

	enc, err := encryptWhole(plaintext, request.sealer, appHeader{})
	require.NoError(t, err)

	d, _, err := openDecrypter(io.NopCloser(bytes.NewReader(enc)), request.opener, testMaxFrameSize)
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()

	got, err := io.ReadAll(iotest.OneByteReader(d))
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)
}

// errAfterReader yields all of data across reads, then returns err.
type errAfterReader struct {
	data []byte
	err  error
}

func (r *errAfterReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}
