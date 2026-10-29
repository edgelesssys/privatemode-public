// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package forwarder

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/persist"
)

// MutationFunc mutates data.
// It is used by [WithRequestMutation] to mutate data read from an [*http.Request].
// It may be called multiple times for a single request to mutate data.
// Return a [Error] to specify the HTTP status on failure.
// Other errors are reported as [http.StatusInternalServerError].
type MutationFunc func(in string) (out string, err error)

// RequestMutator mutates an [*http.Request].
// Return a [Error] to specify the HTTP status on failure.
// Other errors are reported as [http.StatusInternalServerError].
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

// WithRequestMutation returns a [RequestMutator] which passes the entire request body to mutate,
// regardless of its content-type, and replaces the body with the result.
func WithRequestMutation(mutate MutationFunc, log *slog.Logger) RequestMutator {
	return func(r *http.Request) error {
		log.InfoContext(r.Context(), "Mutating full request body")

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

// WithFormRequestMutation returns a [RequestMutator] for requests carrying HTTP form data.
// The mutate function receives the parsed [multipart.Form] and a [multipart.Writer] to write the mutated form to.
// It returns a bool indicating whether mutation was performed. If false, the original request body
// is left unchanged.
func WithFormRequestMutation(mutate func(*multipart.Form, *multipart.Writer) (bool, error), log *slog.Logger) RequestMutator {
	return func(r *http.Request) error {
		log.InfoContext(r.Context(), "Mutating full form request body")

		body, err := persist.ReadBodyUnlimited(r)
		if err != nil {
			return fmt.Errorf("reading request: %w", err)
		}

		boundary, err := parseMultipartBoundaryFromContentType(r.Header.Get("Content-Type"))
		if err != nil {
			return NewError("parsing Content-Type header", err, http.StatusBadRequest)
		}

		reader := multipart.NewReader(bytes.NewReader(body), boundary)
		form, err := reader.ReadForm(constants.MaxFileSizeBytes)
		if err != nil {
			if errors.Is(err, multipart.ErrMessageTooLarge) {
				return fmt.Errorf("parsing form: %w", err)
			}
			// Parse errors quote lines of the body, which is request content.
			return errors.New("parsing form failed")
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
