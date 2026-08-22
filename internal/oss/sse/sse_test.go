// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package sse

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLines(t *testing.T) {
	tests := map[string]struct {
		input string
		want  []Line
	}{
		"single data field": {
			input: "data: hello\n\n",
			want: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte("hello")},
				{Type: LineEnd},
			},
		},
		"comment without space after colon": {
			input: ":comment\n",
			want: []Line{
				{Type: LineComment, Value: []byte("comment")},
			},
		},
		"empty comment": {
			input: ":\n",
			want: []Line{
				{Type: LineComment, Value: []byte{}},
			},
		},
		"field with colon in value": {
			input: "data: hello: world\n",
			want: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte("hello: world")},
			},
		},
		"no trailing newline": {
			input: "data: hello",
			want: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte("hello")},
			},
		},
		"value without space after colon": {
			input: "data:nospace\n",
			want: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte("nospace")},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var got []Line
			for _, line := range parseLines([]byte(tc.input)) {
				got = append(got, line)
			}

			require.Len(t, got, len(tc.want))
			for i := range tc.want {
				assert.Equal(t, tc.want[i].Type, got[i].Type, "line %d type", i)
				assert.Equal(t, tc.want[i].Name, got[i].Name, "line %d name", i)
				assert.Equal(t, tc.want[i].Value, got[i].Value, "line %d value", i)
			}
		})
	}
}

func TestScanEvents(t *testing.T) {
	tests := map[string]struct {
		input string
		want  []string
	}{
		"single LF event": {
			input: "event: x\ndata: hello\n\n",
			want:  []string{"event: x\ndata: hello\n\n"},
		},
		"two events": {
			input: "event: x\ndata: a\n\nevent: x\ndata: b\n\n",
			want:  []string{"event: x\ndata: a\n\n", "event: x\ndata: b\n\n"},
		},
		"two CRLF events": {
			input: "event: x\r\ndata: a\r\n\r\nevent: x\r\ndata: b\r\n\r\n",
			want:  []string{"event: x\r\ndata: a\r\n\r\n", "event: x\r\ndata: b\r\n\r\n"},
		},
		"trailing partial": {
			input: "data: a\n\ndata: partial",
			want:  []string{"data: a\n\n", "data: partial"},
		},
		"empty input": {
			input: "",
			want:  nil,
		},
	}

	readers := map[string]func(string) io.Reader{
		"whole":        func(s string) io.Reader { return strings.NewReader(s) },
		"byte-by-byte": func(s string) io.Reader { return iotest.OneByteReader(strings.NewReader(s)) },
	}

	for name, tc := range tests {
		for readerName, newReader := range readers {
			t.Run(name+"/"+readerName, func(t *testing.T) {
				s := bufio.NewScanner(newReader(tc.input))
				s.Split(ScanEvents)

				var got []string
				for s.Scan() {
					got = append(got, s.Text())
				}
				require.NoError(t, s.Err())
				assert.Equal(t, tc.want, got)
			})
		}
	}
}

func TestParseEvent(t *testing.T) {
	tests := map[string]struct {
		raw            string
		wantEventType  []byte
		wantDispatches bool
		wantFullData   []byte
		wantTerminated bool
		wantLines      []Line
	}{
		"comment-only keepalive": {
			raw:            ": keep-alive\n\n",
			wantDispatches: false,
			wantTerminated: true,
			wantLines: []Line{
				{Type: LineComment, Value: []byte("keep-alive")},
			},
		},
		"event and id only, no data": {
			raw:            "event: foo\nid: 1\n\n",
			wantEventType:  []byte("foo"),
			wantDispatches: false,
			wantTerminated: true,
			wantLines: []Line{
				{Type: LineField, Name: []byte("event"), Value: []byte("foo")},
				{Type: LineField, Name: []byte("id"), Value: []byte("1")},
			},
		},
		"single data line": {
			raw:            "data: hello\n\n",
			wantDispatches: true,
			wantFullData:   []byte("hello"),
			wantTerminated: true,
			wantLines: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte("hello")},
			},
		},
		"empty data value": {
			raw:            "data: \n\n",
			wantDispatches: true,
			wantFullData:   []byte{},
			wantTerminated: true,
			wantLines: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte{}},
			},
		},
		"valueless data field": {
			raw:            "data\n\n",
			wantDispatches: true,
			wantFullData:   []byte{},
			wantTerminated: true,
			wantLines: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte{}},
			},
		},
		"multiple data lines": {
			raw:            "data: a\ndata: b\n\n",
			wantDispatches: true,
			wantFullData:   []byte("a\nb"),
			wantTerminated: true,
			wantLines: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte("a")},
				{Type: LineField, Name: []byte("data"), Value: []byte("b")},
			},
		},
		"multiple empty data lines": {
			raw:            "data:\ndata:\n\n",
			wantDispatches: true,
			wantFullData:   []byte("\n"),
			wantTerminated: true,
			wantLines: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte{}},
				{Type: LineField, Name: []byte("data"), Value: []byte{}},
			},
		},
		"repeated event field, last wins": {
			raw:            "event: a\nevent: b\ndata: x\n\n",
			wantEventType:  []byte("b"),
			wantDispatches: true,
			wantFullData:   []byte("x"),
			wantTerminated: true,
			wantLines: []Line{
				{Type: LineField, Name: []byte("event"), Value: []byte("a")},
				{Type: LineField, Name: []byte("event"), Value: []byte("b")},
				{Type: LineField, Name: []byte("data"), Value: []byte("x")},
			},
		},
		"unknown field, retry and id don't affect the scan": {
			raw:            "custom: value\nretry: 5000\nid: 42\ndata: x\n\n",
			wantDispatches: true,
			wantFullData:   []byte("x"),
			wantTerminated: true,
			wantLines: []Line{
				{Type: LineField, Name: []byte("custom"), Value: []byte("value")},
				{Type: LineField, Name: []byte("retry"), Value: []byte("5000")},
				{Type: LineField, Name: []byte("id"), Value: []byte("42")},
				{Type: LineField, Name: []byte("data"), Value: []byte("x")},
			},
		},
		"CRLF line endings": {
			raw:            "event: foo\r\ndata: hello\r\n\r\n",
			wantEventType:  []byte("foo"),
			wantDispatches: true,
			wantFullData:   []byte("hello"),
			wantTerminated: true,
			wantLines: []Line{
				{Type: LineField, Name: []byte("event"), Value: []byte("foo")},
				{Type: LineField, Name: []byte("data"), Value: []byte("hello")},
			},
		},
		"unterminated event": {
			raw:            "data: hello\n",
			wantDispatches: true,
			wantFullData:   []byte("hello"),
			wantTerminated: false,
			wantLines: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte("hello")},
			},
		},
		"zero-line event": {
			raw:            "\n",
			wantDispatches: false,
			wantTerminated: true,
		},
		"empty raw": {
			raw:            "",
			wantDispatches: false,
			wantTerminated: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			e := ParseEvent([]byte(tc.raw))

			assert.Equal(t, []byte(tc.raw), e.Raw())
			assert.Equal(t, tc.wantEventType, e.Event())
			assert.Equal(t, tc.wantDispatches, e.Dispatches())
			assert.Equal(t, tc.wantFullData, e.FullData())
			assert.Equal(t, tc.wantTerminated, e.Terminated())
			assert.Equal(t, tc.wantLines, collectLines(e))
		})
	}
}

func TestEventReplaceData(t *testing.T) {
	tests := map[string]struct {
		raw  string
		data []byte
		want string
	}{
		"replace at first data line, drop extra data lines, keep other lines": {
			raw:  "event: e\ndata: old1\ndata: old2\nid: 5\n\n",
			data: []byte("new"),
			want: "event: e\ndata: new\nid: 5\n\n",
		},
		"no data lines, terminated: appended before the blank line": {
			raw:  "event: e\nid: 5\n\n",
			data: []byte("new"),
			want: "event: e\nid: 5\ndata: new\n\n",
		},
		"no data lines, unterminated: appended at the end": {
			raw:  "event: e\nid: 5\n",
			data: []byte("new"),
			want: "event: e\nid: 5\ndata: new\n",
		},
		"partial final line gains its LF, terminated-ness preserved": {
			raw:  "data: old\nid: 5",
			data: []byte("new"),
			want: "data: new\nid: 5\n",
		},
		"multi-line data becomes several data lines": {
			raw:  "event: e\ndata: old\n\n",
			data: []byte("line1\nline2"),
			want: "event: e\ndata: line1\ndata: line2\n\n",
		},
		"empty data becomes one empty data line": {
			raw:  "data: old\n\n",
			data: []byte(""),
			want: "data: \n\n",
		},
		"data with a trailing LF gets a trailing empty data line": {
			raw:  "data: old\n\n",
			data: []byte("hello\n"),
			want: "data: hello\ndata: \n\n",
		},
		"empty raw": {
			raw:  "",
			data: []byte("x"),
			want: "data: x\n",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			orig := ParseEvent([]byte(tc.raw))
			got := orig.ReplaceData(tc.data)

			assert.Equal(t, tc.want, string(got.Raw()))
			assert.Equal(t, orig.Terminated(), got.Terminated())
			assert.Equal(t, collectNonDataLines(orig), collectNonDataLines(got))
		})
	}
}

func TestEventReplaceDataFullData(t *testing.T) {
	raws := map[string]string{
		"empty raw":              "",
		"single data line":       "data: old\n\n",
		"non-data fields":        "event: e\nid: 5\n\n",
		"unterminated data line": "data: old\nid: 5",
		"multiple data lines":    "data: a\ndata: b\n\n",
	}
	datas := map[string][]byte{
		"empty data":             []byte(""),
		"single-line data":       []byte("x"),
		"multiline data":         []byte("line1\nline2"),
		"trailing-newline data":  []byte("trailing\n"),
		"empty-middle-line data": []byte("a\n\nb"),
	}

	for rawName, raw := range raws {
		for dataName, data := range datas {
			t.Run(rawName+"/"+dataName, func(t *testing.T) {
				orig := ParseEvent([]byte(raw))
				got := orig.ReplaceData(data)
				assert.Equal(t, data, got.FullData())
			})
		}
	}
}

func TestEventReaderNext(t *testing.T) {
	t.Run("trailing partial event", func(t *testing.T) {
		r := NewEventReader(strings.NewReader("data: a\n\ndata: partial"), 1024)

		e1, err := r.Next()
		require.NoError(t, err)
		assert.True(t, e1.Terminated())
		assert.Equal(t, []byte("a"), e1.FullData())

		e2, err := r.Next()
		require.NoError(t, err)
		assert.False(t, e2.Terminated())
		assert.Equal(t, []byte("partial"), e2.FullData())

		_, err = r.Next()
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("event exceeding maxEventBytes", func(t *testing.T) {
		input := "data: " + strings.Repeat("x", 100) + "\n\n"
		r := NewEventReader(strings.NewReader(input), 20)

		_, err := r.Next()
		require.ErrorIs(t, err, bufio.ErrTooLong)
	})
}

func TestEventStreamRead(t *testing.T) {
	raws := []string{"data: a\n\n", "event: e\ndata: b\n\n", ": keep-alive\n\n"}

	t.Run("ReadAll equals concatenated raw bytes", func(t *testing.T) {
		s := NewEventStream(eventSource(raws...), nil)

		got, err := io.ReadAll(s)
		require.NoError(t, err)
		want := strings.Join(raws, "")
		assert.Equal(t, want, string(got))
	})

	t.Run("unterminated last event is allowed", func(t *testing.T) {
		s := NewEventStream(eventSource("data: a\n\n", "data: partial"), nil)

		got, err := io.ReadAll(s)
		require.NoError(t, err)
		assert.Equal(t, "data: a\n\ndata: partial", string(got))
	})

	t.Run("unterminated event followed by another errors", func(t *testing.T) {
		s := NewEventStream(eventSource("data: partial", "data: b\n\n"), nil)

		_, err := io.ReadAll(s)
		require.ErrorIs(t, err, ErrUnterminatedEvent)
	})
}

func collectLines(e Event) []Line {
	var lines []Line
	for l := range e.Lines() {
		lines = append(lines, l)
	}
	return lines
}

func collectNonDataLines(e Event) []Line {
	var lines []Line
	for l := range e.Lines() {
		if l.IsField(FieldData) {
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

// eventSource returns a next func that yields the events in raws, and then io.EOF.
func eventSource(raws ...string) func() (Event, error) {
	i := 0
	return func() (Event, error) {
		if i >= len(raws) {
			return Event{}, io.EOF
		}
		e := ParseEvent([]byte(raws[i]))
		i++
		return e, nil
	}
}
