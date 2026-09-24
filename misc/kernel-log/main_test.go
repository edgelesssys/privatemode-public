//go:build linux

package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestStream(t *testing.T) {
	first := "6,12,345,-;boot message;with semicolon\n"
	last := "3,19,678,-;NVRM: Xid 62\n DEVICE=+pci:0000:01:00.0\n"
	reader := io.MultiReader(
		strings.NewReader(first),
		&readError{err: unix.EINTR},
		&readError{err: unix.EPIPE},
		strings.NewReader(last),
	)
	var output bytes.Buffer
	if err := stream(reader, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), first+last; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestStreamErrors(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		if err := stream(&readError{err: unix.EACCES}, io.Discard); !errors.Is(err, unix.EACCES) {
			t.Fatalf("error = %v, want EACCES", err)
		}
	})
	t.Run("write", func(t *testing.T) {
		if err := stream(strings.NewReader("6,1,0,-;boot\n"), writeError{}); !errors.Is(err, unix.EPIPE) {
			t.Fatalf("error = %v, want EPIPE", err)
		}
	})
}

type readError struct {
	err error
}

func (r *readError) Read([]byte) (int, error) {
	err := r.err
	r.err = io.EOF
	return 0, err
}

type writeError struct{}

func (writeError) Write([]byte) (int, error) {
	return 0, unix.EPIPE
}
