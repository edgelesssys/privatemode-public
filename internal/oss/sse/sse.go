// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package sse provides a streaming reader for Server-Sent Events (SSE).
//
// The reader is a low-level line parser, not a full EventSource implementation.
//
// It diverges from the WHATWG SSE spec (https://html.spec.whatwg.org/multipage/server-sent-events.html)
// in the following ways:
//   - Bare CR line endings are not supported; only LF and CRLF are recognized.
//   - A leading UTF-8 BOM is not stripped.
//   - Trailing fields at end of stream are yielded, not discarded. The spec
//     requires incomplete events (no final empty line) to be dropped silently.
//     For this reader, that requirement must be implemented on a higher level.
package sse

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"iter"
)

// Well-known SSE field names.
var (
	FieldData  = []byte("data")
	FieldEvent = []byte("event")
	FieldID    = []byte("id")
	FieldRetry = []byte("retry")
)

// LineType represents the kind of SSE line.
type LineType int

const (
	// LineField is a "name: value" or "name" line.
	LineField LineType = iota
	// LineComment is a line starting with ":".
	LineComment
	// LineEnd is an empty line signaling an event boundary.
	LineEnd
)

// Line represents a single parsed SSE line.
// The byte slices are only valid until the next call to [Reader.Next].
type Line struct {
	Type  LineType
	Name  []byte // field name; nil for LineComment and LineEnd
	Value []byte // value after ": "; nil for LineEnd
}

// IsField reports whether the line is a field with the given name.
func (l Line) IsField(name []byte) bool {
	return l.Type == LineField && bytes.Equal(l.Name, name)
}

// Reader yields SSE lines from a stream.
type Reader struct {
	scanner *bufio.Scanner
}

// NewReader creates a new Reader that reads SSE lines from r.
// maxLineBytes sets the maximum allowed line length. Lines exceeding this
// limit cause [Reader.Next] to return [bufio.ErrTooLong].
func NewReader(r io.Reader, maxLineBytes int) *Reader {
	s := bufio.NewScanner(r)
	// Start with a buffer size of <= 64 KiB, which may grow up to maxLineBytes
	s.Buffer(make([]byte, 0, min(maxLineBytes, 64*1024)), maxLineBytes)
	return &Reader{scanner: s}
}

// Next returns the next SSE line. It returns [io.EOF] when the stream ends.
// The returned [Line]'s byte slices should only be used until the next call to Next, which may
// overwrite their contents.
func (r *Reader) Next() (Line, error) {
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return Line{}, err
		}
		return Line{}, io.EOF
	}

	line := r.scanner.Bytes()

	if len(line) == 0 {
		return Line{Type: LineEnd}, nil
	}

	if line[0] == ':' {
		value := line[1:]
		value = bytes.TrimPrefix(value, []byte(" "))
		return Line{Type: LineComment, Value: value}, nil
	}

	name, value, hasColon := bytes.Cut(line, []byte(":"))
	if hasColon {
		value = bytes.TrimPrefix(value, []byte(" "))
	} else {
		// Per the SSE spec, a line without a colon uses the empty string as value.
		// We use nil here to preserve the wire distinction; string(nil) == "".
		value = nil
	}

	return Line{Type: LineField, Name: name, Value: value}, nil
}

// Lines returns an iterator over all SSE lines in the stream, building on [Reader.Next].
// The [Line]'s byte slices are only valid until the iterator advances.
// Iteration stops silently at end of stream. Non-EOF errors are yielded
// as the final element.
func (r *Reader) Lines() iter.Seq2[Line, error] {
	return func(yield func(Line, error) bool) {
		for {
			line, err := r.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				yield(Line{}, err)
				return
			}
			if !yield(line, nil) {
				return
			}
		}
	}
}
