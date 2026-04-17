// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package forwarder

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseMappersDispatchOnContentType(t *testing.T) {
	mappers := map[string]ResponseMapper{
		"passthrough": PassthroughResponseMapper,
		"json":        JSONResponseMapper(markMutateJSONString, nil),
		"raw":         RawResponseMapper(markMutate),
	}

	cases := map[string]struct {
		contentType   string
		wantStreaming bool
	}{
		"json": {"application/json", false},
		"sse":  {"text/event-stream", true},
	}
	for name, tc := range cases {
		for mapperName, mapper := range mappers {
			t.Run(mapperName+"/"+name, func(t *testing.T) {
				//nolint:bodyclose // it's a NopCloser
				upstream := buildResp(tc.contentType, "", `{"a":"hi"}`)
				resp, err := mapper(upstream)
				require.NoError(t, err)
				defer closeMapped(t, upstream, resp)
				_, isStream := resp.(*StreamingResponse)
				assert.Equal(t, tc.wantStreaming, isStream)
			})
		}
	}
}

func TestResponseMappersEncryptedFalseBypassesMutation(t *testing.T) {
	failMutate := func(string) (string, error) {
		return "", errors.New("mutate must not be called")
	}
	bypassMappers := map[string]ResponseMapper{
		"json": JSONResponseMapper(failMutate, nil),
		"raw":  RawResponseMapper(failMutate),
	}
	cases := map[string]struct {
		contentType string
		body        string
	}{
		"unary": {"application/json", `{"a":"hi"}`},
		"sse":   {"text/event-stream", "data: {\"a\":\"hi\"}\n\n"},
	}
	for name, tc := range cases {
		for mapperName, mapper := range bypassMappers {
			t.Run(mapperName+"/"+name, func(t *testing.T) {
				//nolint:bodyclose // it's a NopCloser
				upstream := buildResp(tc.contentType, "false", tc.body)
				resp, err := mapper(upstream)
				require.NoError(t, err)
				defer closeMapped(t, upstream, resp)
				assert.Equal(t, tc.body, readBody(t, resp))
			})
		}
	}
}

func TestResponseMappersStripPerHopHeaders(t *testing.T) {
	mappers := map[string]ResponseMapper{
		"passthrough": PassthroughResponseMapper,
		"json":        JSONResponseMapper(markMutateJSONString, nil),
		"raw":         RawResponseMapper(markMutate),
	}

	for mapperName, mapper := range mappers {
		t.Run(mapperName, func(t *testing.T) {
			upstream := buildResp("application/json", "", `{"a":"hi"}`)
			upstream.Header.Set("Connection", "close")
			upstream.Header.Set("Keep-Alive", "timeout=5")
			upstream.Header.Set("X-Keep-Me", "?1")
			resp, err := mapper(upstream)
			require.NoError(t, err)
			defer closeMapped(t, upstream, resp)
			h := resp.GetHeader()
			assert.Empty(t, h.Get("Connection"))
			assert.Empty(t, h.Get("Keep-Alive"))
			assert.Equal(t, "?1", h.Get("X-Keep-Me"))
		})
	}
}

func TestResponseMappersSSEMutationPerEvent(t *testing.T) {
	cases := map[string]struct {
		mapper ResponseMapper
		body   string
		want   string
	}{
		"json single-field events": {
			mapper: JSONResponseMapper(markMutateJSONString, nil),
			body:   "data: {\"a\":\"hi\"}\n\ndata: {\"a\":\"yo\"}\n\n",
			want:   "data: {\"a\":\"hi|X\"}\n\ndata: {\"a\":\"yo|X\"}\n\n",
		},
		"json multi-field events": {
			mapper: JSONResponseMapper(markMutateJSONString, nil),
			body:   "event: message\ndata: {\"a\":\"hi\"}\n\nevent: error\ndata: {\"a\":\"yo\"}\n\n",
			want:   "event: message\ndata: {\"a\":\"hi|X\"}\n\nevent: error\ndata: {\"a\":\"yo|X\"}\n\n",
		},
		"raw single-field events": {
			mapper: RawResponseMapper(markMutate),
			body:   "data: one\n\ndata: two\n\n",
			want:   "data: one|X\n\ndata: two|X\n\n",
		},
		"raw multi-field events": {
			mapper: RawResponseMapper(markMutate),
			body:   "event: message\ndata: one\n\nevent: error\ndata: two\n\n",
			want:   "event: message\ndata: one|X\n\nevent: error\ndata: two|X\n\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			//nolint:bodyclose // it's a NopCloser
			upstream := buildResp("text/event-stream", "", tc.body)
			resp, err := tc.mapper(upstream)
			require.NoError(t, err)
			defer closeMapped(t, upstream, resp)
			assert.Equal(t, tc.want, readBody(t, resp))
		})
	}
}

func buildResp(contentType, encrypted, body string) *http.Response {
	h := http.Header{}
	h.Set("Content-Type", contentType)
	if encrypted != "" {
		h.Set(privateModeEncryptedHeader, encrypted)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
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

func readBody(t *testing.T, r Response) string {
	t.Helper()
	switch v := r.(type) {
	case *UnaryResponse:
		return string(v.Body)
	case *StreamingResponse:
		var buf bytes.Buffer
		_, err := io.Copy(&buf, v.Body)
		require.NoError(t, err)
		return buf.String()
	}
	t.Fatalf("unexpected response type %T", r)
	return ""
}

func markMutateJSONString(in string) (string, error) {
	return in[:len(in)-1] + `|X"`, nil
}

func markMutate(in string) (string, error) {
	return in + "|X", nil
}
