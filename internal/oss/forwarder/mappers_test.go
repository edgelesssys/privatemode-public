// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package forwarder

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPassthroughResponseMapper(t *testing.T) {
	cases := map[string]struct {
		upstreamContentLength string // empty means header absent
		upstreamStatus        int
		upstreamBody          string
	}{
		"length-set":     {"5", http.StatusOK, "hello"},
		"length-absent":  {"", http.StatusOK, "hello"},
		"non-2xx-status": {"2", http.StatusBadGateway, "{}"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := http.Header{}
			h.Set("Connection", "Keep-Alive")
			h.Set("Another-Header", "My value")
			if tc.upstreamContentLength != "" {
				h.Set("Content-Length", tc.upstreamContentLength)
			}
			upstream := &http.Response{
				StatusCode: tc.upstreamStatus,
				Header:     h,
				Body:       io.NopCloser(strings.NewReader(tc.upstreamBody)),
			}
			resp, err := PassthroughResponseMapper(upstream)
			require.NoError(t, err)
			defer closeMapped(t, upstream, resp)

			streaming, ok := resp.(*StreamingResponse)
			require.True(t, ok, "PassthroughResponseMapper must return *StreamingResponse")

			assert.Equal(t, tc.upstreamStatus, streaming.StatusCode)
			assert.Empty(t, streaming.Header.Get("Connection"))
			assert.Equal(t, "My value", streaming.Header.Get("Another-Header"))
			assert.Equal(t, tc.upstreamContentLength, streaming.Header.Get("Content-Length"))
			body, err := io.ReadAll(streaming.Body)
			require.NoError(t, err)
			assert.Equal(t, tc.upstreamBody, string(body))
		})
	}
}

// closeMapped closes the right body after mapping and reading.
func closeMapped(t *testing.T, upstream *http.Response, r Response) {
	switch v := r.(type) {
	case *StreamingResponse:
		require.NoError(t, v.Body.Close())
		return
	case *UnaryResponse:
		require.NoError(t, upstream.Body.Close())
		return
	}
	t.Fatalf("unexpected response type %T", r)
}
