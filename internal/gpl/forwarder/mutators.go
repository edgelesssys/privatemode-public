// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package forwarder

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	// NestedValue indicates that this field is a nested JSON structure.
	NestedValue Nested = true
	// SimpleValue indicates that this field is a simple string.
	SimpleValue Nested = false
)

// Nested indicates whether a JSON field is a nested JSON structure, or a simple string.
type Nested bool

// FieldSelector is a map of JSON field names to consider for mutation.
// Each entry in the map indicates whether the JSON field is a nested JSON structure or a simple string.
type FieldSelector map[string]Nested

// MutationFunc mutates data.
// It is used by [WithJSONRequestMutation] to mutate data read from an [*http.Request],
// and by [WithJSONResponseMutation] to mutate data read from an [io.Reader].
// It may be called multiple times for a single request to mutate data.
type MutationFunc func(request string) (mutatedRequest string, err error)

// WithJSONRequestMutation returns a [RequestMutator] which performs mutation on the request.
func WithJSONRequestMutation(mutate MutationFunc, fields FieldSelector, log *slog.Logger) RequestMutator {
	return func(r *http.Request) error {
		log.Info("Mutating request")

		req, err := io.ReadAll(r.Body)
		if err != nil {
			return fmt.Errorf("reading request body: %w", err)
		}

		req, err = mutateJSONFields(req, mutate, fields)
		if err != nil {
			return fmt.Errorf("mutating request: %w", err)
		}

		r.ContentLength = int64(len(req)) // mutating input may have altered the length of the request
		r.Body = io.NopCloser(bytes.NewBuffer(req))
		return nil
	}
}

// WithJSONResponseMutation returns a [ResponseMutator] which mutates the data read from the given [io.Reader].
func WithJSONResponseMutation(mutate MutationFunc, fields FieldSelector) ResponseMutator {
	return func(reader io.Reader) io.Reader {
		return &mutatingReader{
			scanner:  bufio.NewScanner(reader),
			mutate:   mutate,
			fields:   fields,
			leftover: nil,
		}
	}
}

// mutatingReader implements a wrapper for an io.ReadCloser,
// which transparently mutates data chunks.
type mutatingReader struct {
	scanner  *bufio.Scanner
	leftover []byte

	fields FieldSelector
	mutate MutationFunc
}

// Read reads from the underlying reader, performs mutation on the data chunks, and returns the mutated data.
func (r *mutatingReader) Read(b []byte) (int, error) {
	// The data read from the underlying reader and/or the final mutated data may be larger than the given buffer
	// In this case, the remaining data is stored and returned on the next call to Read
	if len(r.leftover) > 0 {
		n := copy(b, r.leftover)
		if n < len(r.leftover) {
			r.leftover = r.leftover[n:]
			return n, nil
		}
		r.leftover = nil
		return n, nil
	}
	// Read one chunk of data from the original reader
	// Data chunks are expected to be separated by newlines
	var buf []byte
	if r.scanner.Scan() {
		buf = r.scanner.Bytes()
	} else if r.scanner.Err() != nil {
		return 0, r.scanner.Err()
	} else {
		return 0, io.EOF
	}

	// Skip empty chunks
	if len(buf) == 0 {
		return 0, nil
	}

	// Mutate the data chunk
	mutated, err := mutateJSONFields(buf, r.mutate, r.fields)
	if err != nil {
		return 0, err
	}

	mutated = append(mutated, []byte("\n\n")...) // append newlines which were removed by the scanner

	// Copy the mutated data to the given buffer
	// If the buffer is too small, store the remaining data for the next call to Read
	n := copy(b, mutated)
	if n < len(mutated) {
		r.leftover = mutated[n:]
	}

	return n, nil
}

func mutateJSONFields(data []byte, mutate MutationFunc, fields FieldSelector) ([]byte, error) {
	result := data
	for _, field := range sortedKeys(fields) {
		plainField := gjson.GetBytes(result, field)
		if len(plainField.Array()) == 0 { // skip if the field is empty/does not exist
			continue
		}

		mutatedField, err := mutate(plainField.String())
		if err != nil {
			return nil, fmt.Errorf("mutating request: %w", err)
		}

		if fields[field] { // nested value
			// Use SetRawBytes, as otherwise quotes and data structure characters in the data will be escaped
			result, err = sjson.SetRawBytes(result, field, []byte(mutatedField))
		} else {
			// Otherwise write the plain text as a string
			result, err = sjson.SetBytes(result, field, mutatedField)
		}
		if err != nil {
			return nil, fmt.Errorf("updating input with mutated field: %w", err)
		}
	}
	return result, nil
}

// sortedKeys returns the keys of the given map in alphabetically sorted order.
func sortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	var keys []K
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
