// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package encbody

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/edgelesssys/continuum/internal/oss/oae"
	"github.com/edgelesssys/continuum/internal/oss/sse"
)

// appHeader is carried as the first frame of an encrypted stream.
type appHeader struct {
	// ContentType is the Content-Type of the wrapped plaintext.
	ContentType string `json:"content_type,omitempty"`
	// Headers carries additional authenticated header values.
	Headers map[string]string `json:"headers,omitempty"`
}

// encryptWhole encrypts plaintext as a complete encrypted body. It seals an [appHeader] frame
// followed by a single final body frame.
func encryptWhole(plaintext []byte, sealer *oae.Sealer, h appHeader) ([]byte, error) {
	plainHeader, err := json.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("marshaling app header: %w", err)
	}
	buf, err := appendEncryptedFrame(nil, sealer, plainHeader, false)
	if err != nil {
		return nil, fmt.Errorf("encrypting app header frame: %w", err)
	}
	buf, err = appendEncryptedFrame(buf, sealer, plaintext, true)
	if err != nil {
		return nil, fmt.Errorf("encrypting body frame: %w", err)
	}
	return buf, nil
}

// sseEncrypter encrypts an SSE stream, sealing one event per frame. It implements [io.ReadCloser].
type sseEncrypter struct {
	plaintext io.ReadCloser
	scanner   *bufio.Scanner
	sealer    *oae.Sealer
	buf       []byte // encrypted output awaiting delivery
	pos       int    // delivery offset into buf
	finalDone bool   // was a trailing message with "final" flag sealed?
	err       error  // sticky terminal error, including io.EOF
}

// openSSEEncrypter returns an [sseEncrypter] that seals plaintext events. maxEventBytes bounds each
// event. Oversized events are rejected with a wrapped [bufio.ErrTooLong].
func openSSEEncrypter(plaintext io.ReadCloser, sealer *oae.Sealer, h appHeader, maxEventBytes int) (*sseEncrypter, error) {
	plainHeader, err := json.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("marshaling app header: %w", err)
	}
	buf, err := appendEncryptedFrame(nil, sealer, plainHeader, false)
	if err != nil {
		return nil, fmt.Errorf("encrypting app header frame: %w", err)
	}

	e := &sseEncrypter{
		plaintext: plaintext,
		scanner:   bufio.NewScanner(plaintext),
		sealer:    sealer,
		buf:       buf,
	}
	e.scanner.Split(sse.ScanEvents)
	e.scanner.Buffer(make([]byte, 0, min(64*1024, maxEventBytes)), maxEventBytes)
	return e, nil
}

// Read yields the encrypted body. Returns after each complete frame.
func (e *sseEncrypter) Read(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	if e.pos >= len(e.buf) {
		e.err = e.fill()
		if e.err != nil {
			return 0, e.err
		}
	}
	n := copy(p, e.buf[e.pos:])
	e.pos += n
	return n, nil
}

// fill seals the next stream element into e.buf. It returns io.EOF once the stream is complete.
func (e *sseEncrypter) fill() error {
	e.buf = e.buf[:0]
	e.pos = 0
	if e.scanner.Scan() {
		var err error
		e.buf, err = appendEncryptedFrame(e.buf, e.sealer, e.scanner.Bytes(), false)
		if err != nil {
			return fmt.Errorf("encrypting SSE event frame: %w", err)
		}
		return nil
	}
	if err := e.scanner.Err(); err != nil {
		return fmt.Errorf("reading SSE stream: %w", err)
	}

	if !e.finalDone {
		e.finalDone = true
		var err error
		e.buf, err = appendEncryptedFrame(e.buf, e.sealer, nil, true)
		if err != nil {
			return fmt.Errorf("encrypting final frame: %w", err)
		}
		return nil
	}

	return io.EOF
}

// Close closes the reader.
func (e *sseEncrypter) Close() error {
	return e.plaintext.Close()
}

var _ io.ReadCloser = (*sseEncrypter)(nil)

// decrypter decrypts an encrypted body to plaintext. It implements [io.ReadCloser].
type decrypter struct {
	stream *streamReader
	closer io.Closer
	buf    []byte // current frame's plaintext awaiting delivery
	pos    int    // delivery offset into buf
	err    error  // sticky terminal error, including io.EOF
}

// openDecrypter eagerly reads the [appHeader] frame and returns it with a decrypter for the body.
// The caller must Close the decrypter. maxFrameSize bounds each frame.
func openDecrypter(encrypted io.ReadCloser, opener *oae.Opener, maxFrameSize uint32) (*decrypter, appHeader, error) {
	d := &decrypter{stream: newStreamReader(encrypted, opener, maxFrameSize), closer: encrypted}
	raw, err := d.stream.appendDecryptedFrame(nil)
	if err != nil {
		return nil, appHeader{}, fmt.Errorf("decrypting app header frame: %w", err)
	}
	var h appHeader
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, appHeader{}, fmt.Errorf("unmarshaling app header: %w", err)
	}
	return d, h, nil
}

// Read yields decrypted plaintext. Returns after each non-empty frame.
func (d *decrypter) Read(p []byte) (int, error) {
	if d.err != nil {
		return 0, d.err
	}
	if d.pos < len(d.buf) {
		n := copy(p, d.buf[d.pos:])
		d.pos += n
		return n, nil
	}
	for {
		d.buf, d.err = d.stream.appendDecryptedFrame(d.buf[:0])
		if d.err != nil {
			return 0, d.err
		}
		if len(d.buf) == 0 {
			continue
		}
		d.pos = copy(p, d.buf)
		return d.pos, nil
	}
}

// Close closes the reader.
func (d *decrypter) Close() error {
	return d.closer.Close()
}

var _ io.ReadCloser = (*decrypter)(nil)
