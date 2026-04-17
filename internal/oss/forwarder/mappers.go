// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package forwarder

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/edgelesssys/continuum/internal/oss/constants"
)

// PassthroughResponseMapper forwards the upstream response with unchanged headers and body.
// Per-hop headers are removed.
func PassthroughResponseMapper(resp *http.Response) (Response, error) {
	if isEventStream(resp) {
		return NewStreamingResponseWithHeaders(resp), nil
	}

	r, err := ReadUnaryResponseWithHeaders(resp, constants.MaxUnaryResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("reading upstream response body: %w", err)
	}
	return r, nil
}

// JSONResponseMapper mutates JSON fields in the response body and forwards headers unchanged.
// JSON mutation order is deterministic based on sorting and is the same as for
// [WithJSONRequestMutation]. Per-hop headers are removed.
//
// For streaming (SSE) responses, mutation is applied per-event.
//
// If the Privatemode-Encrypted header is set to "false", this mapper defers to
// [PassthroughResponseMapper].
func JSONResponseMapper(mutate MutationFunc, skipFields FieldSelector) ResponseMapper {
	return func(resp *http.Response) (Response, error) {
		if resp.Header.Get(privateModeEncryptedHeader) == "false" {
			return PassthroughResponseMapper(resp)
		}

		if isEventStream(resp) {
			r := NewStreamingResponseWithHeaders(resp)
			mr := &mutatingReader{
				mutate:        mutate,
				dataParseFunc: MutateJSONFields,
				fields:        skipFields,
			}
			r.Body = mr.Reader(r.Body)
			return r, nil
		}

		r, err := ReadUnaryResponseWithHeaders(resp, constants.MaxUnaryResponseBodyBytes)
		if err != nil {
			return nil, fmt.Errorf("reading upstream response body: %w", err)
		}
		r.Body, err = MutateJSONFields(r.Body, mutate, skipFields)
		if err != nil {
			return nil, fmt.Errorf("mutating response body: %w", err)
		}
		return r, nil
	}
}

// RawResponseMapper mutates the entire response body as single string and forwards headers
// unchanged. Per-hop headers are removed.
//
// For streaming (SSE) responses, mutation is applied per-event.
//
// If the Privatemode-Encrypted header is set to "false", this mapper defers to
// [PassthroughResponseMapper].
func RawResponseMapper(mutate MutationFunc) ResponseMapper {
	return func(resp *http.Response) (Response, error) {
		if resp.Header.Get(privateModeEncryptedHeader) == "false" {
			return PassthroughResponseMapper(resp)
		}

		if isEventStream(resp) {
			r := NewStreamingResponseWithHeaders(resp)
			mr := &mutatingReader{
				mutate: mutate,
				dataParseFunc: func(data []byte, mutate MutationFunc, _ FieldSelector) ([]byte, error) {
					mutated, err := mutate(string(data))
					return []byte(mutated), err
				},
			}
			r.Body = mr.Reader(r.Body)
			return r, nil
		}

		r, err := ReadUnaryResponseWithHeaders(resp, constants.MaxUnaryResponseBodyBytes)
		if err != nil {
			return nil, fmt.Errorf("reading upstream response body: %w", err)
		}
		mutated, err := mutate(string(r.Body))
		if err != nil {
			return nil, fmt.Errorf("mutating response body: %w", err)
		}
		r.Body = []byte(mutated)
		return r, nil
	}
}

// isEventStream reports whether the response is a SSE event stream.
func isEventStream(resp *http.Response) bool {
	return strings.Contains(resp.Header.Get("Content-Type"), "event-stream")
}
