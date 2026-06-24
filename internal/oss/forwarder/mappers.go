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
// Per-hop headers are removed. Body bytes are always streamed through (no explicit distinction
// between upstream unary and streaming) which for downstream HTTP/1.1 results in Transfer-Encoding
// a) identity if the upstream set a Content-Length or b) chunked if not.
func PassthroughResponseMapper(resp *http.Response) (Response, error) {
	// Clone headers directly here. cloneForwardableHeaders() removes Content-Length which we want
	// to retain because the body flows through unchanged.
	header := resp.Header.Clone()
	delHopHeaders(header)
	return &StreamingResponse{
		StatusCode: resp.StatusCode,
		Header:     header,
		Body:       resp.Body,
	}, nil
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
			mr := NewJSONMutatingReader(mutate, skipFields)
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
			mr := NewRawMutatingReader(mutate)
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
