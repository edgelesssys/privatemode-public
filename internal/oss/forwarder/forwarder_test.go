// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package forwarder

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// streamedChunk is written by the stub upstream and is expected by the downstream.
	streamedChunk = "first chunk\n"
	// lateStreamedChunk is written by the stub upstream after the downstream request was cancelled.
	lateStreamedChunk = "late chunk\n"
)

func TestForwardStreaming(t *testing.T) {
	assert := assert.New(t)

	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		for range 20 {
			_, _ = w.Write([]byte(streamedChunk))
			time.Sleep(5 * time.Millisecond)
		}
	}))
	defer stubServer.Close()

	forwarder := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", nil)
	resp := httptest.NewRecorder()

	forwarder.Forward(
		resp,
		req,
		NoRequestMutation,
		PassthroughResponseMapper,
	)

	assert.Equal(http.StatusOK, resp.Code)
	assert.Equal("text/event-stream", resp.Header().Get("Content-Type"))
	assert.Equal(strings.Repeat(streamedChunk, 20), resp.Body.String())
}

func TestForwardStreamingAborted(t *testing.T) {
	assert := assert.New(t)

	sentFirstChunk := make(chan struct{})

	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		// Send a complete chunk
		_, _ = w.Write([]byte(streamedChunk))
		w.(http.Flusher).Flush()

		// Wait before cancelling to let the client start reading
		time.Sleep(10 * time.Millisecond)
		sentFirstChunk <- struct{}{}

		// Send a second chunk only after the downstream request was cancelled
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(lateStreamedChunk))
	}))
	defer stubServer.Close()

	forwarder := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", nil)
	// Wrap req.Context() in a cancelable context
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)
	resp := httptest.NewRecorder()

	go func() {
		<-sentFirstChunk
		cancel()
	}()

	forwarder.Forward(
		resp,
		req,
		NoRequestMutation,
		PassthroughResponseMapper,
	)

	assert.Equal(http.StatusOK, resp.Code)
	assert.Equal("text/event-stream", resp.Header().Get("Content-Type"))

	// Forwarding stops at the cancellation
	assert.Equal(streamedChunk, resp.Body.String())
}

func TestForwardCancelledBeforeResponseReportsClientClosed(t *testing.T) {
	assert := assert.New(t)

	// Upstream never responds; the downstream client gives up first.
	reqReceived := make(chan struct{})
	release := make(chan struct{})
	stubServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		close(reqReceived)
		<-release
	}))
	defer stubServer.Close()
	defer close(release)

	forwarder := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", nil)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)
	resp := httptest.NewRecorder()

	go func() {
		<-reqReceived
		cancel()
	}()

	forwarder.Forward(resp, req, NoRequestMutation, PassthroughResponseMapper)

	// The client is gone, so nothing reads this. It must not land in the 5xx bucket.
	assert.Equal(StatusClientClosedRequest, resp.Code)
	assert.Less(resp.Code, http.StatusInternalServerError)
}

func TestForwardStreamingUpstreamAborts(t *testing.T) {
	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"field\": \"encryptedData\"}\n\n"))
		w.(http.Flusher).Flush()
		// Abort the upstream connection mid-stream so the forwarder's read fails with an
		// unexpected EOF rather than a clean stream end.
		panic(http.ErrAbortHandler)
	}))
	defer stubServer.Close()

	fwd := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", nil)
	resp := httptest.NewRecorder()

	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		fwd.Forward(resp, req, NoRequestMutation, PassthroughResponseMapper)
	})
}

func TestForwardNonStreaming(t *testing.T) {
	assert := assert.New(t)

	mutator := &stubMutator{
		mutateResponse: `{"field": "plainText"}`,
	}

	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// A body larger than a single read buffer
		_, _ = w.Write(fmt.Appendf(nil, "{\"field\": %q}", strings.Repeat("AB", bufio.MaxScanTokenSize)))
	}))
	defer stubServer.Close()

	forwarder := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", nil)
	resp := httptest.NewRecorder()

	forwarder.Forward(
		resp,
		req,
		NoRequestMutation,
		unaryMapper(mutator.mutate),
	)

	assert.Equal(http.StatusOK, resp.Code)
	assert.Equal("application/json", resp.Header().Get("Content-Type"))

	expectedResponse := `{"field": "plainText"}`
	assert.Equal(expectedResponse, resp.Body.String())
}

func TestForwardMutationError(t *testing.T) {
	failingMutator := &stubMutator{
		mutateErr: assert.AnError,
	}

	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"field": "AB"}`))
	}))
	defer stubServer.Close()

	forwarder := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", nil)
	resp := httptest.NewRecorder()

	forwarder.Forward(
		resp,
		req,
		NoRequestMutation,
		unaryMapper(failingMutator.mutate),
	)

	assert := assert.New(t)
	assert.Equal(http.StatusInternalServerError, resp.Code)
}

// unaryMapper maps an upstream response to a [UnaryResponse] with its body mutated as a whole.
func unaryMapper(mutate MutationFunc) ResponseMapper {
	return func(resp *http.Response) (Response, error) {
		r, err := ReadUnaryResponse(resp, constants.MaxUnaryResponseBodyBytes)
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", resp.Header.Get("Content-Type"))
		mutated, err := mutate(string(r.Body))
		if err != nil {
			return nil, err
		}
		r.Body = []byte(mutated)
		return r, nil
	}
}

func TestHTTPError(t *testing.T) {
	tests := map[string]struct {
		acceptHeader        string
		code                int
		msg                 string
		args                []any
		expectedStatusCode  int
		expectedContentType string
		expectedBody        string
	}{
		"Plain request": {
			acceptHeader:        "application/json",
			code:                http.StatusInternalServerError, // 500
			msg:                 "Internal error occurred",
			args:                nil,
			expectedStatusCode:  http.StatusInternalServerError,
			expectedContentType: "application/json",
			expectedBody:        `{"error":{"message":"Internal error occurred","type":""}}`,
		},
		"SSE request": {
			acceptHeader:        "text/event-stream",
			code:                http.StatusBadRequest, // 400
			msg:                 "Invalid parameter",
			args:                nil,
			expectedStatusCode:  http.StatusBadRequest,
			expectedContentType: "text/event-stream",
			expectedBody:        "event: error\n\ndata: {\"error\":{\"message\":\"Invalid parameter\",\"type\":\"\"}}\n\n",
		},
		"Plain request with args": {
			acceptHeader:        "",                  // No accept header
			code:                http.StatusNotFound, // 404
			msg:                 "Resource %s not found",
			args:                []any{"item123"},
			expectedStatusCode:  http.StatusNotFound,
			expectedContentType: "application/json",
			expectedBody:        `{"error":{"message":"Resource item123 not found","type":""}}`,
		},
		"SSE request with args": {
			acceptHeader:        "text/event-stream",
			code:                http.StatusUnauthorized, // 401
			msg:                 "User %d unauthorized",
			args:                []any{42},
			expectedStatusCode:  http.StatusUnauthorized,
			expectedContentType: "text/event-stream",
			expectedBody:        "event: error\n\ndata: {\"error\":{\"message\":\"User 42 unauthorized\",\"type\":\"\"}}\n\n",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			if tt.acceptHeader != "" {
				req.Header.Set("Accept", tt.acceptHeader)
			}

			HTTPError(rr, req, tt.code, tt.msg, tt.args...)

			resp := rr.Result()
			bodyBytes, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			bodyString := string(bodyBytes)

			assert.Equal(t, tt.expectedStatusCode, resp.StatusCode, "Status code mismatch")
			assert.Equal(t, tt.expectedContentType, resp.Header.Get("Content-Type"), "Content-Type header mismatch")

			assert.Equal(t, tt.expectedBody, bodyString, "Body mismatch")
		})
	}
}

func TestForwardMaxBodyBytes(t *testing.T) {
	const maxBytes = 10

	testCases := map[string]struct {
		body               string
		expectedStatusCode int
	}{
		"allowed request": {
			body:               "small",
			expectedStatusCode: http.StatusOK,
		},
		"too large request": {
			body:               "this body is way too large",
			expectedStatusCode: http.StatusRequestEntityTooLarge,
		},
	}

	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stubServer.Close()

	fwd := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, slog.Default())

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/test", strings.NewReader(tc.body))
			resp := httptest.NewRecorder()

			fwd.Forward(
				resp, req,
				NoRequestMutation, PassthroughResponseMapper,
				WithMaxBodyBytes(maxBytes, ""),
			)

			assert.Equal(t, tc.expectedStatusCode, resp.Code)
		})
	}
}

func TestForwardRetry(t *testing.T) {
	testCases := map[string]struct {
		delay            time.Duration
		shouldRetry      bool
		expectedCode     int
		expectedAttempts int
		errorBody        string
	}{
		"no delay":                            {0, true, http.StatusOK, 3, ""},
		"with delay":                          {10 * time.Millisecond, true, http.StatusOK, 3, ""},
		"no retry":                            {0, false, http.StatusInternalServerError, 1, ""},
		"error body at limit":                 {0, true, http.StatusOK, 3, strings.Repeat("x", maxErrorBodyBytes)},
		"error body over limit":               {0, true, http.StatusOK, 3, strings.Repeat("x", maxErrorBodyBytes+1)},
		"error body over limit without retry": {0, false, http.StatusBadGateway, 1, strings.Repeat("x", maxErrorBodyBytes+1)},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			// Track attempts and responses
			attemptCount := 0
			responses := []struct {
				statusCode int
				body       string
			}{
				{500, "Internal Server Error"},
				{500, "Still failing"},
				{200, `{"success": true}`},
			}
			if tc.errorBody != "" {
				responses[0].body = tc.errorBody
			}

			stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if attemptCount < len(responses) {
					response := responses[attemptCount]
					w.WriteHeader(response.statusCode)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(response.body))
				} else {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte("Unexpected attempt"))
				}
				attemptCount++
			}))
			defer stubServer.Close()

			forwarder := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, slog.Default())

			callbackCount := 0
			retryCallback := func(statusCode int, errMsg string, attempt int) (bool, time.Duration) {
				callbackCount++

				// Verify we get the correct response body for each attempt
				expectedBodies := []string{responses[0].body, "Still failing"}
				if len(tc.errorBody) > maxErrorBodyBytes {
					expectedBodies[0] = "upstream error response body too large"
				}
				if attempt <= len(expectedBodies) {
					assert.Equal(expectedBodies[attempt-1], errMsg, "Response body should match expected for attempt %d", attempt)
				}
				assert.Equal(http.StatusInternalServerError, statusCode)
				if !tc.shouldRetry {
					return false, 0
				}

				shouldRetry := statusCode == 500 && attempt < 3
				return shouldRetry, tc.delay
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/test", nil)
			resp := httptest.NewRecorder()

			startTime := time.Now()
			forwarder.Forward(
				resp,
				req,
				NoRequestMutation,
				PassthroughResponseMapper,
				WithRetryCallback(retryCallback),
			)
			elapsed := time.Since(startTime)

			assert.Equal(tc.expectedCode, resp.Code)
			assert.Equal(tc.expectedAttempts, attemptCount, "Should have made exactly %d attempts", tc.expectedAttempts)
			if tc.shouldRetry {
				assert.Equal(2, callbackCount)
			} else {
				assert.Equal(1, callbackCount)
			}

			if tc.expectedCode == http.StatusOK {
				assert.Equal(`{"success": true}`, resp.Body.String())
			}

			if tc.delay > 0 && tc.shouldRetry {
				// Should have waited for 2 retries * delay = minimum delay
				expectedMinDelay := 2 * tc.delay
				assert.GreaterOrEqual(elapsed, expectedMinDelay, "Should have waited at least %v for delays", expectedMinDelay)
			}
		})
	}
}

// TestForwardDoesNotStripTraceparent verifies that requests entering with a
// traceparent header have it forwarded to the upstream untouched. This guards
// against accidental inclusion of traceparent in delHopHeaders.
func TestForwardDoesNotStripTraceparent(t *testing.T) {
	const inbound = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"

	var gotTraceparent string
	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("traceparent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer stubServer.Close()

	// Use a plain http.Client (no otelhttp) - we only want to assert the
	// header survives the forwarder's hop-by-hop pruning.
	fwd := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("traceparent", inbound)
	resp := httptest.NewRecorder()
	fwd.Forward(resp, req, NoRequestMutation, PassthroughResponseMapper)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, inbound, gotTraceparent)
}

func TestForwardDeletesHopHeaders(t *testing.T) {
	assert := assert.New(t)

	var got *http.Request
	stubServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r
	}))
	defer stubServer.Close()

	upstreamHost := stubServer.Listener.Addr().String()
	fwd := New(http.DefaultClient, upstreamHost, SchemeHTTP, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", nil)
	req.Host = "endpoint.intranet.example.com"
	req.Header.Set("Connection", "Keep-Alive, X-Connection-Scoped")
	req.Header.Set("X-Connection-Scoped", "secret")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("Trailer", "X-Checksum")
	req.Header.Set("X-App-Header", "kept")

	resp := httptest.NewRecorder()
	fwd.Forward(resp, req, NoRequestMutation, PassthroughResponseMapper)

	assert.Equal(http.StatusOK, resp.Code)
	if !assert.NotNil(got) {
		return
	}
	assert.Equal(upstreamHost, got.Host)
	// All these hop headers should be dropped
	assert.Empty(got.Header.Get("Connection"))
	assert.Empty(got.Header.Get("Keep-Alive"))
	assert.Empty(got.Header.Get("X-Connection-Scoped"))
	assert.Empty(got.Header.Get("Trailer"))
	// Other headers are kept
	assert.Equal("kept", got.Header.Get("X-App-Header"))
}

// roundTripFunc adapts a function to an [http.RoundTripper] to inject transport errors.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// timeoutError is a [net.Error] that reports itself as a timeout.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestForwardTransportErrorStatus(t *testing.T) {
	tests := map[string]struct {
		transportErr error
		wantStatus   int
	}{
		"connection failure -> 502 Bad Gateway": {
			transportErr: &net.OpError{Op: "dial", Err: errors.New("connection refused")},
			wantStatus:   http.StatusBadGateway,
		},
		"timeout -> 504 Gateway Timeout": {
			transportErr: &net.OpError{Op: "dial", Err: timeoutError{}},
			wantStatus:   http.StatusGatewayTimeout,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, tc.transportErr
			})}
			fwd := New(client, "upstream.invalid:443", SchemeHTTPS, slog.Default())

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", nil)
			resp := httptest.NewRecorder()
			fwd.Forward(resp, req, NoRequestMutation, PassthroughResponseMapper)

			assert.Equal(t, tc.wantStatus, resp.Code)
		})
	}
}

func TestExtractFlusher(t *testing.T) {
	// httptest.ResponseRecorder implements http.Flusher.
	w := dummyWriterWrapper{httptest.NewRecorder()}
	f, ok := extractFlusher(w)
	assert.True(t, ok)
	assert.NotNil(t, f)
}

// dummyWriterWrapper wraps an [http.ResponseWriter].
type dummyWriterWrapper struct {
	http.ResponseWriter
}

func (u dummyWriterWrapper) Unwrap() http.ResponseWriter { return u.ResponseWriter }
