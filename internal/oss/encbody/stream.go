// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package encbody

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/edgelesssys/continuum/internal/oss/oae"
)

var errFrameTooLarge = errors.New("frame exceeds maximum size")

// framePrefixLen is the byte length of a frame's big-endian uint32 length prefix.
const framePrefixLen = 4

// appendEncryptedFrame encrypts plaintext with sealer and appends it as a length-prefixed frame to
// dst, returning the extended slice. On error, dst is returned with its original contents.
func appendEncryptedFrame(dst []byte, sealer *oae.Sealer, plaintext []byte, final bool) ([]byte, error) {
	encSize := len(plaintext) + oae.SealedOverheadSize

	if encSize > math.MaxUint32 {
		return dst, fmt.Errorf("%w: %d (encrypted) bytes", errFrameTooLarge, encSize)
	}

	base := len(dst)
	// Allocate capacity for full frame to avoid multiple later allocations
	dst = slices.Grow(dst, framePrefixLen+encSize)
	dst = binary.BigEndian.AppendUint32(dst, uint32(encSize))

	dst, err := sealer.SealMessage(dst, plaintext, final)
	if err != nil {
		return dst[:base], fmt.Errorf("sealing message: %w", err)
	}
	return dst, nil
}

// streamReader reads an encrypted stream frame by frame. It is not safe for concurrent use.
type streamReader struct {
	r            *bufio.Reader
	maxFrameSize uint32
	opener       *oae.Opener
	sealedBuf    []byte               // reused buffer for sealed frames
	prefixBuf    [framePrefixLen]byte // reused buffer for the length prefix
}

func newStreamReader(r io.Reader, opener *oae.Opener, maxFrameSize uint32) *streamReader {
	return &streamReader{r: bufio.NewReader(r), maxFrameSize: maxFrameSize, opener: opener}
}

// appendFramePayload is an internal helper which reads the next length-prefixed frame and appends its
// payload to dst. Returns io.EOF when the stream ends cleanly on a frame boundary and
// io.ErrUnexpectedEOF when it ends mid-frame. On error, dst is returned with its original contents
// and the stream is left in an undefined state.
func (s *streamReader) appendFramePayload(dst []byte) ([]byte, error) {
	if _, err := io.ReadFull(s.r, s.prefixBuf[:]); err != nil {
		return dst, err
	}
	size := binary.BigEndian.Uint32(s.prefixBuf[:])
	if size > s.maxFrameSize {
		return dst, fmt.Errorf("%w: %d bytes (max %d)", errFrameTooLarge, size, s.maxFrameSize)
	}

	base := len(dst)
	dst = slices.Grow(dst, int(size))[:base+int(size)]
	if _, err := io.ReadFull(s.r, dst[base:]); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return dst[:base], err
	}
	return dst, nil
}

// appendDecryptedFrame reads the next frame, decrypts it, and appends the plaintext to dst,
// returning the extended slice. It returns io.EOF when the stream ended cleanly after a final
// message. On error, dst is returned with its original contents and the stream is left in an
// undefined state.
func (s *streamReader) appendDecryptedFrame(dst []byte) ([]byte, error) {
	var err error
	s.sealedBuf, err = s.appendFramePayload(s.sealedBuf[:0])
	if err != nil {
		// Finish is required to terminate the OAE stream and enforce truncation detection. Only
		// propagate its error on clean EOF.
		ferr := s.opener.Finish()
		if errors.Is(err, io.EOF) && ferr != nil {
			return dst, ferr
		}
		return dst, err
	}

	opened, err := s.opener.OpenMessage(dst, s.sealedBuf)
	if err != nil {
		return dst, fmt.Errorf("opening sealed message: %w", err)
	}
	return opened, nil
}
