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
	"mime/multipart"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	eventStreamSuffix    = "\n\n"
	eventStreamSeparator = ": "
)

// FieldSelector is a list of field names to consider for mutation.
//
// For form requests, each entry is expected to be a slice of length 1,
// where each entry is a name of a form field.
// For example, the FieldSelector{["foo"], ["bar"]} matches the form fields "foo" and "bar".
//
// For JSON requests, each entry represents a JSON path as a slice of strings,
// where each string in the slice is a field name of the path.
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
type MutationFunc func(in string) (out string, err error)

// RequestMutator mutates an [*http.Request].
type RequestMutator func(request *http.Request) error

// HeaderMutator mutates a [http.Header].
// response is the header object of the response. Writes should usually go here.
// request is the header object of the request.
type HeaderMutator func(response http.Header, request http.Header) error

// RequestMutatorChain is a chain of [RequestMutator]s.
func RequestMutatorChain(
	mutators ...RequestMutator,
) RequestMutator {
	return func(request *http.Request) error {
		for _, mutator := range mutators {
			if err := mutator(request); err != nil {
				return fmt.Errorf("mutating request: %w", err)
			}
		}
		return nil
	}
}

// WithFullRequestMutation returns a [RequestMutator] which performs mutation on the entire request body,
// regardless of its content-type. It uses the provided MutationFunc to mutate the raw request body.
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

// WithFullJSONRequestMutation returns a [RequestMutator] which mutates the full request.
func WithFullJSONRequestMutation(mutate MutationFunc, skipFields FieldSelector, log *slog.Logger) RequestMutator {
	return withJSONRequestMutation(mutate, skipFields, mutateAllJSONFields, log)
}

// WithFormRequestMutation returns a [RequestMutator] which mutates requests with HTTP form data.
func WithFormRequestMutation(mutate MutationFunc, skipFields FieldSelector, log *slog.Logger) RequestMutator {
	return func(r *http.Request) error {
		log.Info("Mutating HTTP form request")

		if err := r.ParseMultipartForm(64 * 1024 * 1024); err != nil {
			return fmt.Errorf("parsing form: %w", err)
		}

		formValueKeys := make([]string, 0, len(r.MultipartForm.Value))
		for key := range r.MultipartForm.Value {
			formValueKeys = append(formValueKeys, key)
		}
		sort.StringSlice(formValueKeys).Sort()

		formFileKeys := make([]string, 0, len(r.MultipartForm.File))
		for key := range r.MultipartForm.File {
			formFileKeys = append(formFileKeys, key)
		}

		mutatedBody := &bytes.Buffer{}
		writer := multipart.NewWriter(mutatedBody)

		// Copy form values
		for _, formKey := range formValueKeys {
			log.Info("Mutating form field", "key", formKey)
			if err := mutateFormField(writer, formKey, r.FormValue(formKey), mutate, skipFields); err != nil {
				return fmt.Errorf("mutating form field %q: %w", formKey, err)
			}
		}

		// Copy form files
		for _, fileKey := range formFileKeys {
			log.Info("Mutating form file", "key", fileKey)
			formFile, _, err := r.FormFile(fileKey)
			if err != nil {
				return fmt.Errorf("getting form file %q: %w", fileKey, err)
			}
			if err := mutateFormFile(writer, fileKey, formFile, mutate, skipFields); err != nil {
				return fmt.Errorf("mutating form file %q: %w", fileKey, err)
			}
		}

		if err := writer.Close(); err != nil {
			return fmt.Errorf("closing writer: %w", err)
		}

		r.Header.Set("Content-Type", writer.FormDataContentType())
		r.ContentLength = int64(mutatedBody.Len())
		r.Body = io.NopCloser(mutatedBody)

		return nil
	}
}

// WithSelectJSONResponseMutation returns a [ResponseMutator] which mutates the data read from the given [io.Reader].
func WithSelectJSONResponseMutation(mutate MutationFunc, fields FieldSelector) ResponseMutator {
	return &mutatingReader{
		scanner:        nil,
		mutate:         mutate,
		dataParseFunc:  mutateSelectJSONFields,
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
		dataParseFunc:  mutateAllJSONFields,
		fields:         skipFields,
		leftover:       nil,
		skipFinalEvent: backwardsCompatibleMode,
	}
}

// WithFullResponseMutation returns a [ResponseMutator] which mutates the whole body of the data from the given [io.Reader].
func WithFullResponseMutation(mutate MutationFunc) ResponseMutator {
	return &mutatingReader{
		scanner: nil,
		mutate:  mutate,
		dataParseFunc: func(data []byte, mutate MutationFunc, _ FieldSelector) ([]byte, error) {
			mutated, err := mutate(string(data))
			return []byte(mutated), err
		},
		fields:         nil,
		leftover:       nil,
		skipFinalEvent: false,
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

// mutatingReader implements a wrapper for an [io.ReadCloser],
// which transparently mutates data chunks.
type mutatingReader struct {
	scanner  *bufio.Scanner
	leftover []byte

	// workaround required for backwards compatibility with privatemode-proxy < v1.16
	skipFinalEvent bool

	fields        FieldSelector
	mutate        MutationFunc
	dataParseFunc func(data []byte, mutate MutationFunc, fields FieldSelector) ([]byte, error)
}

// Reader returns a mutating [io.Reader].
func (r *mutatingReader) Reader(reader io.Reader) io.Reader {
	r.scanner = bufio.NewScanner(reader)
	return r
}

// Mutate performs mutation on the given data.
func (r *mutatingReader) Mutate(body []byte) ([]byte, error) {
	return r.dataParseFunc(body, r.mutate, r.fields)
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
		mutated, err = r.dataParseFunc(toMutate, r.mutate, r.fields)
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
			subPaths := EvaluateArrayPaths(gjson.GetBytes(result, field), mutateField[1:])
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
				subPaths = append(subPaths, EvaluateArrayPaths(gjson.GetBytes(result, field), skipField[1:])...)
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

// EvaluateArrayPaths expands a JSON path using the '#' array index placeholder
// to multiple JSON paths with the actual array indices.
func EvaluateArrayPaths(gjsonData gjson.Result, path []string) FieldSelector {
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

func mutateFormField(
	writer *multipart.Writer, formKey, formValue string, mutate MutationFunc, skipFields FieldSelector,
) error {
	if slices.ContainsFunc(skipFields, func(skip []string) bool {
		return len(skip) > 0 && skip[0] == formKey
	}) {
		if err := writer.WriteField(formKey, formValue); err != nil {
			return fmt.Errorf("writing form field %q: %w", formKey, err)
		}
		return nil
	}

	mutatedValue, err := mutate(formValue)
	if err != nil {
		return fmt.Errorf("mutating form field %q: %w", formKey, err)
	}

	if err := writer.WriteField(formKey, mutatedValue); err != nil {
		return fmt.Errorf("writing form field %q: %w", formKey, err)
	}

	return nil
}

func mutateFormFile(
	writer *multipart.Writer, formKey string, formFile multipart.File, mutate MutationFunc, skipFields FieldSelector,
) (err error) {
	formWriter, err := writer.CreateFormFile(formKey, formKey)
	if err != nil {
		return fmt.Errorf("creating form file %q: %w", formKey, err)
	}
	defer func() {
		if closeErr := formFile.Close(); err != nil {
			err = errors.Join(err, fmt.Errorf("closing form file %q: %w", formKey, closeErr))
		}
	}()

	if slices.ContainsFunc(skipFields, func(skip []string) bool {
		return len(skip) > 0 && skip[0] == formKey
	}) {
		if _, err := io.Copy(formWriter, formFile); err != nil {
			return fmt.Errorf("copying form file %q: %w", formKey, err)
		}
		return nil
	}

	formData, err := io.ReadAll(formFile)
	if err != nil {
		return fmt.Errorf("reading form file %q: %w", formKey, err)
	}

	mutatedData, err := mutate(string(formData))
	if err != nil {
		return fmt.Errorf("mutating form file %q: %w", formKey, err)
	}
	if _, err := formWriter.Write([]byte(mutatedData)); err != nil {
		return fmt.Errorf("writing form file %q: %w", formKey, err)
	}

	return nil
}
