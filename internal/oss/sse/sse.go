// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package sse provides a streaming reader for Server-Sent Events (SSE).
//
// The reader is a low-level event parser, not a full EventSource implementation.
//
// It diverges from the WHATWG SSE spec (https://html.spec.whatwg.org/multipage/server-sent-events.html)
// in the following ways:
//   - Bare CR line endings are not supported; only LF and CRLF are recognized.
//   - A leading UTF-8 BOM is not stripped.
//   - Unterminated events at end of stream are yielded, not discarded. The spec
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
// The byte slices share memory with the raw input they were parsed from.
type Line struct {
	Type  LineType
	Name  []byte // field name; nil for LineComment and LineEnd
	Value []byte // value after ": "; nil for LineEnd
}

// IsField reports whether the line is a field with the given name.
func (l Line) IsField(name []byte) bool {
	return l.Type == LineField && bytes.Equal(l.Name, name)
}

// parseLine parses the content of a single SSE line, with the line terminator removed.
func parseLine(line []byte) Line {
	if len(line) == 0 {
		return Line{Type: LineEnd}
	}

	if line[0] == ':' {
		value := line[1:]
		value = bytes.TrimPrefix(value, []byte(" "))
		return Line{Type: LineComment, Value: value}
	}

	name, value, hasColon := bytes.Cut(line, []byte(":"))
	if hasColon {
		value = bytes.TrimPrefix(value, []byte(" "))
	} else {
		// Per the SSE spec, a line without a colon uses the empty string as value.
		value = []byte{}
	}

	return Line{Type: LineField, Name: name, Value: value}
}

// ScanEvents is a [bufio.SplitFunc] that yields one SSE event per scanner token, including its
// terminating blank line and LF/CRLF line endings. It returns a partial event on EOF detectable by
// its missing terminating blank line.
func ScanEvents(data []byte, atEOF bool) (advance int, token []byte, err error) {
	lineStart := 0
	for i := range data {
		if data[i] != '\n' {
			continue
		}
		contentEnd := i
		if contentEnd > lineStart && data[contentEnd-1] == '\r' {
			// ignore CR of a CRLF terminator
			contentEnd--
		}
		if contentEnd == lineStart {
			// blank line ends the event
			return i + 1, data[:i+1], nil
		}
		lineStart = i + 1
	}
	// If at EOF, return a partial event
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	// no full event found, signal "need more data"
	return 0, nil, nil
}

// Event is an immutable view over the raw bytes of a single SSE event. An Event and all byte slices
// obtained from it share memory with the raw input and are only valid as long as the input is
// valid.
type Event struct {
	raw        []byte
	terminated bool

	// Results of the shallow scan:

	eventType []byte // value of the last "event" field; nil if absent
	firstData []byte // value of the first "data" field; nil if absent
	dataLines int    // number of "data" field lines
}

// ParseEvent parses raw as a single SSE event. raw must contain at most one blank line as its final
// line. [ScanEvents] is a recommended source for raw.
func ParseEvent(raw []byte) Event {
	e := Event{raw: raw}
	for _, line := range parseLines(raw) {
		e.terminated = line.Type == LineEnd
		if line.Type != LineField {
			continue
		}
		switch {
		case bytes.Equal(line.Name, FieldData):
			if e.dataLines == 0 {
				e.firstData = line.Value
			}
			e.dataLines++
		case bytes.Equal(line.Name, FieldEvent):
			// Last occurrence wins per the spec.
			e.eventType = line.Value
		}
	}
	return e
}

// Raw returns the event's wire bytes, including terminating blank line (if [Event.Terminated]).
func (e *Event) Raw() []byte {
	return e.raw
}

// Terminated reports whether the event includes its terminating blank line.
func (e *Event) Terminated() bool {
	return e.terminated
}

// Event returns the value of the event's last "event" field, or nil if absent.
func (e *Event) Event() []byte {
	return e.eventType
}

// Dispatches reports whether the event has at least one data line. Such events should be dispatched
// to listeners per the spec. It doesn't consider [Event.Terminated], which the spec also requires.
func (e *Event) Dispatches() bool {
	return e.dataLines > 0
}

// FullData returns the event's full data. nil is returned when there are no data lines. It is
// zero-copy if there is at most one data line.
func (e *Event) FullData() []byte {
	if e.dataLines <= 1 {
		return e.firstData
	}
	data := make([]byte, 0, len(e.raw))
	first := true
	for line := range e.Lines() {
		if !line.IsField(FieldData) {
			continue
		}
		if !first {
			data = append(data, '\n')
		}
		first = false
		data = append(data, line.Value...)
	}
	return data
}

// Lines iterates the event's lines in wire order, excluding the terminating blank line.
func (e *Event) Lines() iter.Seq[Line] {
	return func(yield func(Line) bool) {
		for _, line := range parseLines(e.raw) {
			if line.Type == LineEnd {
				continue
			}
			if !yield(line) {
				return
			}
		}
	}
}

// ReplaceData returns a new Event with data replacing the event's data lines. Other lines are
// copied verbatim, with their terminators normalized to LF. A partial final line gains a
// terminator. Event terminated-ness is kept. data must not contain CR characters.
func (e *Event) ReplaceData(data []byte) Event {
	// The allocated capacity is just an estimate
	raw := make([]byte, 0, len(e.raw)-len(e.firstData)+len(data)+len(fieldDataPrefix)+1)
	dataWritten := false
	for rawLine, line := range parseLines(e.raw) {
		if line.Type == LineEnd {
			// Restored from e.terminated below.
			continue
		}
		if line.IsField(FieldData) {
			if !dataWritten {
				raw = appendDataLines(raw, data)
				dataWritten = true
			}
			continue
		}
		raw = append(raw, rawLine...)
		raw = append(raw, '\n')
	}
	if !dataWritten {
		raw = appendDataLines(raw, data)
	}
	if e.terminated {
		raw = append(raw, '\n')
	}
	return ParseEvent(raw)
}

var fieldDataPrefix = []byte("data: ")

// appendDataLines appends data as one or more "data: " lines, splitting on LF.
func appendDataLines(dst, data []byte) []byte {
	for {
		line, rest, more := bytes.Cut(data, []byte("\n"))
		dst = append(dst, fieldDataPrefix...)
		dst = append(dst, line...)
		dst = append(dst, '\n')
		if !more {
			return dst
		}
		data = rest
	}
}

// parseLines iterates over the lines of raw, yielding each line's content without its terminator
// together with the parsed line. A blank line is yielded as a [LineEnd] line. A partial final line
// is yielded as if terminated.
func parseLines(raw []byte) iter.Seq2[[]byte, Line] {
	return func(yield func([]byte, Line) bool) {
		remaining := raw
		for len(remaining) > 0 {
			var line []byte
			if i := bytes.IndexByte(remaining, '\n'); i < 0 {
				line, remaining = remaining, nil
			} else {
				line, remaining = remaining[:i+1], remaining[i+1:]
			}
			content := trimLineEnding(line)
			if !yield(content, parseLine(content)) {
				return
			}
		}
	}
}

// trimLineEnding removes a trailing LF or CRLF from line.
func trimLineEnding(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte("\n"))
	return bytes.TrimSuffix(line, []byte("\r"))
}

// EventReader yields SSE events from an [io.Reader].
type EventReader struct {
	scanner *bufio.Scanner
}

// NewEventReader creates an EventReader reading from r. maxEventBytes limits the size of each event
// and [bufio.ErrTooLong] is returned if exceeded.
func NewEventReader(r io.Reader, maxEventBytes int) *EventReader {
	s := bufio.NewScanner(r)
	// Start with a buffer size of <= 64 KiB, which may grow up to maxEventBytes
	s.Buffer(make([]byte, 0, min(maxEventBytes, 64*1024)), maxEventBytes)
	s.Split(ScanEvents)
	return &EventReader{scanner: s}
}

// Next returns the next SSE event, or [io.EOF] when the stream ends. The returned Event and every
// slice obtained from it are only valid until the next call to Next.
func (r *EventReader) Next() (Event, error) {
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return Event{}, err
		}
		return Event{}, io.EOF
	}
	return ParseEvent(r.scanner.Bytes()), nil
}

// Events returns an iterator over the reader's events, building on [EventReader.Next]. The yielded
// Event is only valid until the iterator advances.
func (r *EventReader) Events() iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		for {
			event, err := r.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				yield(Event{}, err)
				return
			}
			if !yield(event, nil) {
				return
			}
		}
	}
}

// ErrUnterminatedEvent is returned by [EventStream.Read] when the source yields an unterminated
// event followed by another event.
var ErrUnterminatedEvent = errors.New("sse: unterminated event followed by another event")

// EventStream is an [io.ReadCloser], serializing an event source to a byte stream.
type EventStream struct {
	next   func() (Event, error)
	closer io.Closer
	buf    []byte // wire bytes of the current event awaiting delivery
	err    error  // sticky terminal error, including io.EOF

	wroteUnterminated bool // last served event was unterminated, nothing may follow it
}

// NewEventStream creates a new EventStream. next must return [io.EOF] at the end of the stream, and
// an event that isn't [Event.Terminated] must be its last. An event's raw bytes are fully
// delivered before next is called again, so next may reuse buffers between calls. Close closes
// closer, if non-nil.
func NewEventStream(next func() (Event, error), closer io.Closer) *EventStream {
	return &EventStream{next: next, closer: closer}
}

// Read yields the byte stream of SSE events. Returns after each event.
func (s *EventStream) Read(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	if len(s.buf) == 0 {
		s.err = s.fill()
		if s.err != nil {
			return 0, s.err
		}
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

// fill stages the next event's raw bytes for delivery, or returns an error.
func (s *EventStream) fill() error {
	event, err := s.next()
	if err != nil {
		return err
	}
	if s.wroteUnterminated {
		return ErrUnterminatedEvent
	}
	s.buf = event.Raw()
	s.wroteUnterminated = !event.Terminated()
	return nil
}

// Close closes the underlying closer, if not nil.
func (s *EventStream) Close() error {
	if s.closer == nil {
		return nil
	}
	return s.closer.Close()
}

var _ io.ReadCloser = (*EventStream)(nil)
