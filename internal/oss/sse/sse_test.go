// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package sse

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReaderNext(t *testing.T) {
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
		"multiple fields in one event": {
			input: "event: update\ndata: payload l1\ndata: payload l2\nid: 42\n\n",
			want: []Line{
				{Type: LineField, Name: []byte("event"), Value: []byte("update")},
				{Type: LineField, Name: []byte("data"), Value: []byte("payload l1")},
				{Type: LineField, Name: []byte("data"), Value: []byte("payload l2")},
				{Type: LineField, Name: []byte("id"), Value: []byte("42")},
				{Type: LineEnd},
			},
		},
		"comment line": {
			input: ": this is a comment\n\n",
			want: []Line{
				{Type: LineComment, Value: []byte("this is a comment")},
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
		"field without value": {
			input: "data\n\n",
			want: []Line{
				{Type: LineField, Name: []byte("data"), Value: nil},
				{Type: LineEnd},
			},
		},
		"field with empty value": {
			input: "data: \n",
			want: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte{}},
			},
		},
		"field with colon in value": {
			input: "data: hello: world\n",
			want: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte("hello: world")},
			},
		},
		"unknown field name": {
			input: "custom: value\n\n",
			want: []Line{
				{Type: LineField, Name: []byte("custom"), Value: []byte("value")},
				{Type: LineEnd},
			},
		},
		"multiple events": {
			input: "data: first\n\ndata: second\n\n",
			want: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte("first")},
				{Type: LineEnd},
				{Type: LineField, Name: []byte("data"), Value: []byte("second")},
				{Type: LineEnd},
			},
		},
		"no trailing newline": {
			input: "data: hello",
			want: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte("hello")},
			},
		},
		"empty input": {
			input: "",
			want:  nil,
		},
		"value without space after colon": {
			input: "data:nospace\n",
			want: []Line{
				{Type: LineField, Name: []byte("data"), Value: []byte("nospace")},
			},
		},
		"CRLF line endings": {
			input: "event: update\r\ndata: payload\r\n\r\n",
			want: []Line{
				{Type: LineField, Name: []byte("event"), Value: []byte("update")},
				{Type: LineField, Name: []byte("data"), Value: []byte("payload")},
				{Type: LineEnd},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			r := NewReader(strings.NewReader(tc.input), 1024)
			var got []Line

			for {
				line, err := r.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				// Copy slices since they're only valid until the next call.
				got = append(got, Line{
					Type:  line.Type,
					Name:  bytes.Clone(line.Name),
					Value: bytes.Clone(line.Value),
				})
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

func TestReaderLineTooLong(t *testing.T) {
	input := "data: " + strings.Repeat("x", 100) + "\n"
	r := NewReader(strings.NewReader(input), 50)

	_, err := r.Next()
	require.ErrorIs(t, err, bufio.ErrTooLong)
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
