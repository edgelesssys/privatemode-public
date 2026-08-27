// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package forwarder

import (
	"net/http"
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
