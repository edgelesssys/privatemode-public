// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package forwarder

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// FieldSelector is a list of JSON field names to consider for mutation.
// JSON paths are represented as a list of strings, where each string is a field name.
//
// For example, the FieldSelector{["foo", "bar"]} creates the JSON path "foo.bar",
// and would match the field "bar" in the object "foo".
//
// The special character '#' is used to indicate that all elements of an array should be considered.
// For example, FieldSelector{["foo", "bar", "#", "baz"]} creates the JSON path "foo.bar.#.baz",
// and would match all elements "baz" of the array "bar" in the object "foo", e.g. "foo.bar.0.baz", "foo.bar.1.baz", etc.
// If only specific array elements should be selected, they can be directly addressed by their index.
// For example, FieldSelector{["foo", "bar", "0", "baz"]} creates the JSON path "foo.bar.0.baz",
// and would only match the first element "baz" of the array "bar" in the object "foo".
type FieldSelector [][]string

// MutationFunc mutates data.
// It is used by [WithJSONRequestMutation] to mutate data read from an [*http.Request],
// and by [WithJSONResponseMutation] to mutate data read from an [io.Reader].
// It may be called multiple times for a single request to mutate data.
type MutationFunc func(request string) (mutatedRequest string, err error)

// WithSelectJSONRequestMutation returns a [RequestMutator] which mutates the request.
func WithSelectJSONRequestMutation(mutate MutationFunc, fields FieldSelector, log *slog.Logger) RequestMutator {
	return withJSONRequestMutation(mutate, fields, mutateSelectJSONFields, log)
}

// WithFullRequestMutation returns a [RequestMutator] which performs mutation on the entire request body,
// regardless of content type. It uses the provided MutationFunc to mutate the raw request body.
func WithFullRequestMutation(mutate MutationFunc, log *slog.Logger) RequestMutator {
	return func(r *http.Request) error {
		log.Info("Mutating full request body")

		bodyBytes, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			return fmt.Errorf("reading request body: %w", err)
		}

		mutatedStr, err := mutate(string(bodyBytes))
		if err != nil {
			return fmt.Errorf("mutating request: %w", err)
		}

		mutatedBytes := []byte(mutatedStr)
		r.ContentLength = int64(len(mutatedBytes))
		r.Body = io.NopCloser(bytes.NewBuffer(mutatedBytes))

		return nil
	}
}

// WithFullJSONRequestMutation returns a [RequestMutator] which mutates the request.
func WithFullJSONRequestMutation(mutate MutationFunc, skipFields FieldSelector, log *slog.Logger) RequestMutator {
	return withJSONRequestMutation(mutate, skipFields, mutateAllJSONFields, log)
}

const (
	eventStreamSuffix    = "\n\n"
	eventStreamSeparator = ": "
)

// WithSelectJSONResponseMutation returns a [ResponseMutator] which mutates the data read from the given [io.Reader].
func WithSelectJSONResponseMutation(mutate MutationFunc, fields FieldSelector) ResponseMutator {
	return &mutatingReader{
		scanner:        nil,
		mutate:         mutate,
		jsonParseFunc:  mutateSelectJSONFields,
		fields:         fields,
		leftover:       nil,
		skipFinalEvent: false,
	}
}

// WithFullJSONResponseMutation returns a [ResponseMutator] which mutates the data read from the given [io.Reader].
func WithFullJSONResponseMutation(mutate MutationFunc, skipFields FieldSelector, backwardsCompatibleMode bool) ResponseMutator {
	return &mutatingReader{
		scanner:        nil,
		mutate:         mutate,
		jsonParseFunc:  mutateAllJSONFields,
		fields:         skipFields,
		leftover:       nil,
		skipFinalEvent: backwardsCompatibleMode,
	}
}

func withJSONRequestMutation(
	mutate MutationFunc, fields FieldSelector,
	mutateFunc func([]byte, MutationFunc, FieldSelector) ([]byte, error),
	log *slog.Logger,
) RequestMutator {
	return func(r *http.Request) error {
		log.Info("Mutating request")

		req, err := io.ReadAll(r.Body)
		if err != nil {
			return fmt.Errorf("reading request body: %w", err)
		}

		// Allow empty requests
		if len(req) > 0 && !gjson.ValidBytes(req) {
			return errors.New("invalid JSON data")
		}

		req, err = mutateFunc(req, mutate, fields)
		if err != nil {
			return fmt.Errorf("mutating request: %w", err)
		}

		r.ContentLength = int64(len(req)) // mutating input may have altered the length of the request
		r.Body = io.NopCloser(bytes.NewBuffer(req))
		return nil
	}
}

// mutatingReader implements a wrapper for an io.ReadCloser,
// which transparently mutates data chunks.
type mutatingReader struct {
	scanner  *bufio.Scanner
	leftover []byte

	// workaround required for backwards compatibility with privatemode-proxy < v1.16
	skipFinalEvent bool

	fields        FieldSelector
	mutate        MutationFunc
	jsonParseFunc func(data []byte, mutate MutationFunc, fields FieldSelector) ([]byte, error)
}

// Reader returns a mutating [io.Reader].
func (r *mutatingReader) Reader(reader io.Reader) io.Reader {
	r.scanner = bufio.NewScanner(reader)
	return r
}

// Mutate performs mutation on the given data.
func (r *mutatingReader) Mutate(body []byte) ([]byte, error) {
	return r.jsonParseFunc(body, r.mutate, r.fields)
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

	// Remove the event stream prefix, since it breaks JSON parsing
	bufCpy := make([]byte, len(buf))
	copy(bufCpy, buf)
	before, after, found := bytes.Cut(bufCpy, []byte(eventStreamSeparator))
	var toMutate, prefix []byte
	if found {
		// Copy the values over to avoid working on the original buffer
		// since [bytes.Cut] returns slices of the original buffer
		toMutate = make([]byte, len(after))
		copy(toMutate, after)
		prefix = append(before, []byte(eventStreamSeparator)...)
	} else {
		toMutate = make([]byte, len(before))
		copy(toMutate, before)
	}

	var mutated []byte
	var err error
	// Skip the final "[DONE]"" event, since it's not a JSON object we can mutate
	if bytes.EqualFold(toMutate, []byte("[DONE]")) && !r.skipFinalEvent {
		mutated = toMutate
	} else {
		// Mutate the data chunk
		mutated, err = r.jsonParseFunc(toMutate, r.mutate, r.fields)
		if err != nil {
			return 0, err
		}
	}

	// Add back event stream prefix and append newlines which were removed by the scanner
	mutated = append(prefix, append(mutated, []byte(eventStreamSuffix)...)...)

	// Copy the mutated data to the given buffer
	// If the buffer is too small, store the remaining data for the next call to Read
	n := copy(b, mutated)
	if n < len(mutated) {
		r.leftover = mutated[n:]
	}

	return n, nil
}

func mutateSelectJSONFields(data []byte, mutate MutationFunc, mutateFields FieldSelector) ([]byte, error) {
	result := data
	for _, field := range sortedIndices(data) {
		var mutateField []string
		for _, prospectiveField := range mutateFields {
			if field == prospectiveField[0] {
				mutateField = prospectiveField
				break
			}
		}

		// Skip if there was no match for the current field in our selector
		if len(mutateField) == 0 {
			continue
		}

		// If we terminated the JSON path, i.e. only one element left in the selector for the current field,
		// use the mutation function supplied by the caller
		mutateFunc := mutate
		// Otherwise recursively call [mutateSelectJSONFields]
		if len(mutateField) > 1 {
			subPaths := evaluateArrayPaths(gjson.GetBytes(result, field), mutateField[1:])
			mutateFunc = func(data string) (string, error) {
				mutatedField, err := mutateSelectJSONFields([]byte(data), mutate, subPaths)
				if err != nil {
					return "", fmt.Errorf("mutating nested field: %w", err)
				}
				return string(mutatedField), nil
			}
		}

		// Mutate the field
		mutatedField, err := mutateFunc(gjson.GetBytes(result, field).Raw)
		if err != nil {
			return nil, fmt.Errorf("mutating field %q: %w", field, err)
		}

		// Use SetRawBytes, as otherwise quotes and data structure characters in the data will be escaped
		result, err = sjson.SetRawBytes(result, field, []byte(mutatedField))
		if err != nil {
			return nil, fmt.Errorf("updating input with mutated field: %w", err)
		}
	}
	return result, nil
}

func mutateAllJSONFields(data []byte, mutate MutationFunc, skipFields FieldSelector) ([]byte, error) {
	if len(data) != 0 && !gjson.ValidBytes(data) {
		return nil, errors.New("mutation on invalid JSON data")
	}

	// Collect all top level indices of the given JSON data
	indices := sortedIndices(data)

	result := data
	for _, field := range indices {
		skip := false
		subPaths := FieldSelector{}
		for _, skipField := range skipFields {
			// Check if the current field should be skipped
			if len(skipField) == 1 && skipField[0] == field {
				skip = true
				break
			}

			// Check if any subfields of the current field should be skipped
			if len(skipField) > 1 && skipField[0] == field {
				subPaths = append(subPaths, evaluateArrayPaths(gjson.GetBytes(result, field), skipField[1:])...)
			}
		}
		if skip {
			continue
		}

		// By default, use the mutation function supplied by the caller
		mutateFunc := mutate
		// If a subfield should be skipped, recursively call mutateJSONFields
		if len(subPaths) > 0 {
			mutateFunc = func(data string) (string, error) {
				mutatedField, err := mutateAllJSONFields([]byte(data), mutate, subPaths)
				if err != nil {
					return "", fmt.Errorf("mutating nested field: %w", err)
				}
				return string(mutatedField), nil
			}
		}

		// Mutate the field
		mutatedField, err := mutateFunc(gjson.GetBytes(result, field).Raw)
		if err != nil {
			return nil, fmt.Errorf("mutating field %q: %w", field, err)
		}

		// Use SetRawBytes, as otherwise quotes and data structure characters in the data will be escaped
		result, err = sjson.SetRawBytes(result, field, []byte(mutatedField))
		if err != nil {
			return nil, fmt.Errorf("updating input with mutated field: %w", err)
		}
	}
	return result, nil
}

// evaluateArrayPaths expands a JSON path using the '#' array index placeholder
// to multiple JSON paths with the actual array indices.
func evaluateArrayPaths(gjsonData gjson.Result, path []string) FieldSelector {
	var subPaths FieldSelector
	switch {
	case gjsonData.IsObject(), gjsonData.IsArray() && path[0] != "#":
		// In case of a nested object, or in case of an array and our JSON path specifying only a specific index,
		// simply add the subfield to the list of fields
		subPaths = FieldSelector{path}
	case gjsonData.IsArray() && path[0] == "#":
		// In case of an array, and in case the JSON path wants to iterate over all elements of the array,
		// create one JSON path for each element of the array
		fieldArray := gjsonData.Array()
		for i := range fieldArray {
			nestedArrayFields := []string{}
			if len(path) > 1 {
				nestedArrayFields = path[1:]
			}
			subPaths = append(subPaths, append([]string{strconv.Itoa(i)}, nestedArrayFields...))
		}
	}
	return subPaths
}

func sortedIndices(jsonData []byte) []string {
	indices := []string{}
	gjson.ParseBytes(jsonData).ForEach(func(key, _ gjson.Result) bool {
		// Escape wildcards and dots inside key names
		// Required to select the correct value when using [gjson.GetBytes]
		escapedKey := strings.ReplaceAll(key.String(), ".", "\\.")
		escapedKey = strings.ReplaceAll(escapedKey, "?", "\\?")
		escapedKey = strings.ReplaceAll(escapedKey, "*", "\\*")
		indices = append(indices, escapedKey)
		return true
	})
	sort.StringSlice(indices).Sort()
	return indices
}
