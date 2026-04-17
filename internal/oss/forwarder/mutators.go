// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package forwarder

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/persist"
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
// and by [JSONResponseMapper] to mutate data read from an [io.Reader].
// It may be called multiple times for a single request to mutate data.
type MutationFunc func(in string) (out string, err error)

// RequestMutator mutates an [*http.Request].
type RequestMutator func(request *http.Request) error

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

// MutationFuncChain is a chain of [MutationFunc]s.
func MutationFuncChain(
	mutators ...MutationFunc,
) MutationFunc {
	return func(in string) (string, error) {
		out := in
		for _, mutator := range mutators {
			var err error
			out, err = mutator(out)
			if err != nil {
				return "", fmt.Errorf("mutating data: %w", err)
			}
		}
		return out, nil
	}
}

// WithRawRequestMutation returns a [RequestMutator] which performs mutation on the entire request body,
// regardless of its content-type. It uses the provided MutationFunc to mutate the raw request body.
func WithRawRequestMutation(mutate MutationFunc, log *slog.Logger) RequestMutator {
	return func(r *http.Request) error {
		log.Info("Mutating full request body")

		bodyBytes, err := persist.ReadBodyUnlimited(r)
		if err != nil {
			return fmt.Errorf("reading request body: %w", err)
		}

		mutatedStr, err := mutate(string(bodyBytes))
		if err != nil {
			return fmt.Errorf("mutating request: %w", err)
		}

		persist.SetBody(r, []byte(mutatedStr))
		return nil
	}
}

// WithJSONRequestMutation returns a [RequestMutator] which mutates the full request.
// Mutation order is deterministic based on sorting and is the same as for [JSONResponseMapper].
func WithJSONRequestMutation(mutate MutationFunc, skipFields FieldSelector, log *slog.Logger) RequestMutator {
	return withJSONRequestMutation(mutate, skipFields, MutateJSONFields, log)
}

// WithRawFormRequestMutation mutates the entire body of requests with HTTP form data.
// The mutate function receives the parsed [multipart.Form] and a [multipart.Writer] to write the mutated form to.
// It returns a bool indicating whether mutation was performed. If false, the original request body
// is left unchanged.
func WithRawFormRequestMutation(mutate func(*multipart.Form, *multipart.Writer) (bool, error), log *slog.Logger) RequestMutator {
	return func(r *http.Request) error {
		log.Info("Mutating full form request body")

		body, err := persist.ReadBodyUnlimited(r)
		if err != nil {
			return fmt.Errorf("reading request: %w", err)
		}

		boundary, err := parseMultipartBoundaryFromContentType(r.Header.Get("Content-Type"))
		if err != nil {
			return fmt.Errorf("parsing Content-Type header: %w", err)
		}

		reader := multipart.NewReader(bytes.NewReader(body), boundary)
		form, err := reader.ReadForm(constants.MaxFileSizeBytes)
		if err != nil {
			return fmt.Errorf("parsing form: %w", err)
		}
		defer func() { _ = form.RemoveAll() }()

		mutatedBody := &bytes.Buffer{}
		writer := multipart.NewWriter(mutatedBody)

		mutated, err := mutate(form, writer)
		if err != nil {
			return fmt.Errorf("mutating form: %w", err)
		}
		if !mutated {
			return nil
		}

		if err := writer.Close(); err != nil {
			return fmt.Errorf("closing writer: %w", err)
		}

		r.Header.Set("Content-Type", writer.FormDataContentType())
		// We know that mutatedBody (a bytes.Buffer) is no longer modified, so its internal buffer can be passed on
		persist.SetBody(r, mutatedBody.Bytes())
		return nil
	}
}

// WithFormRequestMutation mutates each individual field in requests with HTTP form data.
// Mutation order is deterministic: fields in ascending name order, then files in ascending filename order.
func WithFormRequestMutation(mutate MutationFunc, skipFields FieldSelector, log *slog.Logger) RequestMutator {
	innerMutator := WithRawFormRequestMutation(func(form *multipart.Form, writer *multipart.Writer) (bool, error) {
		formValueKeys := make([]string, 0, len(form.Value))
		for key := range form.Value {
			formValueKeys = append(formValueKeys, key)
		}
		sort.StringSlice(formValueKeys).Sort()

		formFileKeys := make([]string, 0, len(form.File))
		for key := range form.File {
			formFileKeys = append(formFileKeys, key)
		}
		sort.StringSlice(formFileKeys).Sort()

		// Copy form values
		for _, formKey := range formValueKeys {
			log.Info("Mutating form field", "key", formKey)
			values := form.Value[formKey]
			if len(values) == 0 {
				continue
			}
			if err := mutateFormField(writer, formKey, values[0], mutate, skipFields); err != nil {
				return false, fmt.Errorf("mutating form field %q: %w", formKey, err)
			}
		}

		// Copy form files
		for _, fileKey := range formFileKeys {
			log.Info("Mutating form file", "key", fileKey)
			files := form.File[fileKey]
			if len(files) == 0 {
				continue
			}
			formFile, err := files[0].Open()
			if err != nil {
				return false, fmt.Errorf("opening form file %q: %w", fileKey, err)
			}
			// mutateFormFile() always closes formFile
			if err := mutateFormFile(writer, fileKey, formFile, mutate, skipFields); err != nil {
				return false, fmt.Errorf("mutating form file %q: %w", fileKey, err)
			}
		}

		return true, nil
	}, log)

	return func(r *http.Request) error {
		log.Info("Mutating HTTP form request")
		return innerMutator(r)
	}
}

func withJSONRequestMutation(
	mutate MutationFunc, fields FieldSelector,
	mutateFunc func([]byte, MutationFunc, FieldSelector) ([]byte, error),
	log *slog.Logger,
) RequestMutator {
	return func(r *http.Request) error {
		log.Info("Mutating request")

		req, err := persist.ReadBodyUnlimited(r)
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

		persist.SetBody(r, req)
		return nil
	}
}

// mutatingReader implements a wrapper for an [io.ReadCloser],
// which transparently mutates data chunks.
type mutatingReader struct {
	scanner  *bufio.Scanner
	leftover []byte
	closer   io.Closer

	fields        FieldSelector
	mutate        MutationFunc
	dataParseFunc func(data []byte, mutate MutationFunc, fields FieldSelector) ([]byte, error)
}

// Reader returns a mutating [io.Reader]. Close cascades to the wrapped reader.
func (r *mutatingReader) Reader(reader io.ReadCloser) io.ReadCloser {
	r.scanner = bufio.NewScanner(reader)
	r.closer = reader
	return r
}

// Close closes the wrapped reader.
func (r *mutatingReader) Close() error {
	return r.closer.Close()
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
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return 0, err
		}
		return 0, io.EOF
	}
	buf := r.scanner.Bytes()

	// Skip empty chunks
	if len(buf) == 0 {
		return 0, nil
	}

	mutated, err := r.mutateChunk(buf)
	if err != nil {
		return 0, err
	}

	// Copy the mutated data to the given buffer
	// If the buffer is too small, store the remaining data for the next call to Read
	n := copy(b, mutated)
	if n < len(mutated) {
		r.leftover = mutated[n:]
	}

	return n, nil
}

// WriteTo implements the [io.WriterTo] interface, allowing direct writing
// of the mutated data to an [io.Writer], improving performance for copy operations.
// Data is read, one line (chunk) at a time, from a pre-configured [bufio.Scanner], mutated,
// and written to the provided [io.Writer].
func (r *mutatingReader) WriteTo(w io.Writer) (n int64, err error) {
	if r.scanner == nil {
		return 0, errors.New("mutatingReader: no data to write")
	}
	for r.scanner.Scan() {
		nPartial, err := r.writeTo(w, r.scanner.Bytes())
		if err != nil {
			return n, err
		}
		n += nPartial
	}
	if err := r.scanner.Err(); err != nil {
		return n, err
	}
	return n, nil
}

// writeTo writes a single chunk of (mutated) data to the given [io.Writer].
func (r *mutatingReader) writeTo(w io.Writer, b []byte) (int64, error) {
	// Skip empty chunks
	if len(b) == 0 {
		return 0, nil
	}

	mutated, err := r.mutateChunk(b)
	if err != nil {
		return 0, err
	}

	n, err := w.Write(mutated)
	if err != nil {
		return int64(n), err
	}
	return int64(n), nil
}

// mutateChunk parses and mutates a single data chunk.
func (r *mutatingReader) mutateChunk(b []byte) ([]byte, error) {
	// Remove the event stream prefix, since it breaks JSON parsing
	bufCpy := make([]byte, len(b))
	copy(bufCpy, b)
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
	// Skip SSE "event:" lines (used in Anthropic format) - they don't contain JSON.
	// Also skip the final "[DONE]" event (used in OpenAI format), since it's not a JSON object we can mutate.
	isEventLine := found && bytes.Equal(before, []byte("event"))
	isDoneEvent := bytes.EqualFold(toMutate, []byte("[DONE]"))
	if isEventLine || isDoneEvent {
		mutated = toMutate
	} else {
		// Mutate the data chunk
		mutated, err = r.dataParseFunc(toMutate, r.mutate, r.fields)
		if err != nil {
			return nil, err
		}
	}

	// TODO: refactor mutatingReader to parse SSE at the event level (splitting on \n\n) rather
	// than the line level (splitting on \n), so that event structure is preserved end-to-end
	// without needing per-field-type heuristics like the one below.

	// Add back event stream prefix and append newlines which were removed by the scanner.
	// For Anthropic "event:" lines, use a single newline — the following "data:" line is part
	// of the same event and must not be separated by a blank line.
	suffix := eventStreamSuffix
	if isEventLine {
		suffix = "\n"
	}
	mutated = append(prefix, append(mutated, []byte(suffix)...)...)
	return mutated, nil
}

// isValidJSON returns nil if data is valid JSON or empty, an "incomplete JSON" error if incomplete, or a formatted error otherwise.
func isValidJSON(data []byte) error {
	if len(data) == 0 || gjson.ValidBytes(data) {
		return nil
	}
	trimmed := bytes.TrimSpace(data)
	err := errors.New("invalid JSON data")
	if (bytes.HasPrefix(trimmed, []byte("{")) && !bytes.HasSuffix(trimmed, []byte("}"))) ||
		(bytes.HasPrefix(trimmed, []byte("[")) && !bytes.HasSuffix(trimmed, []byte("]"))) {
		err = errors.New("incomplete JSON")
	}
	return fmt.Errorf("mutation on invalid JSON data: %w", err)
}

// MutateJSONFields mutates all JSON fields in data, skipping fields matched by skipFields.
func MutateJSONFields(data []byte, mutate MutationFunc, skipFields FieldSelector) ([]byte, error) {
	if err := isValidJSON(data); err != nil {
		return nil, err
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
				mutatedField, err := MutateJSONFields([]byte(data), mutate, subPaths)
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
) (retErr error) {
	defer func() {
		if closeErr := formFile.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("closing form file %q: %w", formKey, closeErr))
		}
	}()
	formWriter, err := writer.CreateFormFile(formKey, formKey)
	if err != nil {
		return fmt.Errorf("creating form file %q: %w", formKey, err)
	}

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

func parseMultipartBoundaryFromContentType(contentType string) (string, error) {
	mediatype, params, err := mime.ParseMediaType(contentType)
	if err != nil || (mediatype != "multipart/form-data" && mediatype != "multipart/mixed") {
		return "", http.ErrNotMultipart
	}
	boundary, ok := params["boundary"]
	if !ok {
		return "", http.ErrMissingBoundary
	}
	return boundary, nil
}
