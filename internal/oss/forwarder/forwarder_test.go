// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package forwarder

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
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

	forwarder := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, UpstreamInference, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, constants.OpenAIChatCompletionsEndpoint, nil)
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

	forwarder := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, UpstreamInference, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, constants.OpenAIChatCompletionsEndpoint, nil)
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

	forwarder := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, UpstreamInference, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, constants.OpenAIChatCompletionsEndpoint, nil)
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

	var logs bytes.Buffer
	fwd := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP,
		UpstreamInference,
		slog.New(slog.NewJSONHandler(&logs, nil)))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, constants.OpenAIChatCompletionsEndpoint, nil)
	resp := httptest.NewRecorder()

	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		fwd.Forward(resp, req, NoRequestMutation, PassthroughResponseMapper)
	})
	assert.Contains(t, logs.String(), `"error":"streaming response body: unexpected EOF"`)
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

	forwarder := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, UpstreamInference, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, constants.OpenAIChatCompletionsEndpoint, nil)
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

	forwarder := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, UpstreamInference, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, constants.OpenAIChatCompletionsEndpoint, nil)
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
			expectedBody:        "event: error\ndata: {\"error\":{\"message\":\"Invalid parameter\",\"type\":\"\"}}\n\n",
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
			expectedBody:        "event: error\ndata: {\"error\":{\"message\":\"User 42 unauthorized\",\"type\":\"\"}}\n\n",
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

func TestHTTPErrorWithDetails(t *testing.T) {
	for _, accept := range []string{"application/json", "text/event-stream"} {
		t.Run(accept, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			req.Header.Set("Accept", accept)
			resp := httptest.NewRecorder()
			HTTPErrorWithDetails(resp, req, http.StatusNotFound,
				"The model 'unknown' does not exist.",
				"invalid_request_error", "model", "model_not_found",
			)

			assert.Equal(t, http.StatusNotFound, resp.Code)
			assert.Equal(t, accept, resp.Header().Get("Content-Type"))
			assert.Equal(t, "nosniff", resp.Header().Get("X-Content-Type-Options"))
			body := resp.Body.String()
			if accept == "text/event-stream" {
				require.True(t, strings.HasPrefix(body, "event: error\ndata: "))
				require.True(t, strings.HasSuffix(body, "\n\n"))
				body = strings.TrimPrefix(body, "event: error\ndata: ")
			}
			assert.JSONEq(t, `{"error":{"message":"The model 'unknown' does not exist.",
				"type":"invalid_request_error","param":"model","code":"model_not_found"}}`, body)
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

	fwd := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, UpstreamInference, slog.Default())

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

func TestForwardMutationErrorStatus(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
		for _, retryEnabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("status=%d/retry=%t", status, retryEnabled), func(t *testing.T) {
				var retryCallback RetryCallback
				if retryEnabled {
					retryCallback = func(int, string, int) (bool, time.Duration) {
						t.Error("mutation errors must not trigger retries")
						return false, 0
					}
				}
				fwd := New(nil, "upstream", SchemeHTTP, UpstreamInference, slog.Default())
				req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
				resp := httptest.NewRecorder()
				fwd.Forward(resp, req, func(*http.Request) error {
					return fmt.Errorf("wrapped: %w", NewError("mutation failed", errors.New("invalid input"), status))
				}, PassthroughResponseMapper, WithRetryCallback(retryCallback))
				assert.Equal(t, status, resp.Code)
				assert.Contains(t, resp.Body.String(), "mutation failed")
			})
		}
	}
}

func TestForwardErrorMapper(t *testing.T) {
	for name, tc := range map[string]struct {
		mapErr   error
		wantBody string
	}{
		"mapped": {
			wantBody: `mapped 400 {"error":{"message":"forwarding request: mutating request: mutation failed: secret input","type":""}}`,
		},
		"mapper fails": {
			mapErr:   errors.New("mapping failed"),
			wantBody: `{"error":{"message":"internal error: mapping error response failed","type":""}}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			fwd := New(nil, "upstream", SchemeHTTP, UpstreamInference, slog.New(slog.DiscardHandler))
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
			resp := httptest.NewRecorder()
			mapper := func(r *UnaryResponse) (Response, error) {
				if tc.mapErr != nil {
					return nil, tc.mapErr
				}
				return &UnaryResponse{
					StatusCode: r.StatusCode,
					Header:     http.Header{"Content-Type": {"text/plain"}},
					Body:       fmt.Appendf(nil, "mapped %d %s", r.StatusCode, r.Body),
				}, nil
			}
			fwd.Forward(resp, req, func(*http.Request) error {
				return NewError("mutation failed", errors.New("secret input"), http.StatusBadRequest)
			}, PassthroughResponseMapper, WithErrorMapper(mapper))

			assert.Equal(t, http.StatusBadRequest, resp.Code)
			assert.Equal(t, tc.wantBody, resp.Body.String())
		})
	}
}

func TestForwardLogsOmitSensitiveInput(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "secret-query", req.URL.Query().Get("prompt"))
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "upstream-error")
	}))
	defer upstream.Close()
	for _, stage := range []string{"upstream", "mutation", "mapping"} {
		t.Run(stage, func(t *testing.T) {
			var logs bytes.Buffer
			fwd := New(upstream.Client(), upstream.Listener.Addr().String(), SchemeHTTP,
				UpstreamInference,
				slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
				constants.OpenAIChatCompletionsEndpoint+"?prompt=secret-query", strings.NewReader("secret-prompt"))
			resp := httptest.NewRecorder()
			fwd.Forward(resp, req, func(*http.Request) error {
				if stage == "mutation" {
					return errors.New("secret-mutation-error")
				}
				return nil
			}, func(resp *http.Response) (Response, error) {
				if stage == "mapping" {
					return nil, errors.New("secret-decrypted-response")
				}
				return PassthroughResponseMapper(resp)
			}, WithRetryCallback(func(_ int, msg string, _ int) (bool, time.Duration) {
				assert.Equal(t, "upstream-error", msg)
				return false, 0
			}))
			assert.NotContains(t, logs.String(), "secret-")
			assert.Contains(t, logs.String(), `"path":"`+constants.OpenAIChatCompletionsEndpoint+`"`)
			assert.NotContains(t, logs.String(), "upstream-error")
			if stage != "mutation" {
				assert.Contains(t, logs.String(), `"statusCode":400`)
			}
			if stage == "upstream" {
				assert.Equal(t, http.StatusBadRequest, resp.Code)
				assert.Equal(t, "upstream-error", resp.Body.String())
			}
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

			// Claimed at the start of each handler call: the forwarder stops reading an
			// oversized error body early and retries, so the next attempt may reach the
			// server before the previous handler call has returned.
			var attemptCount atomic.Int32
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
				attempt := int(attemptCount.Add(1)) - 1
				if attempt < len(responses) {
					response := responses[attempt]
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(response.statusCode)
					_, _ = w.Write([]byte(response.body))
				} else {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte("Unexpected attempt"))
				}
			}))
			defer stubServer.Close()

			forwarder := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, UpstreamInference, slog.Default())

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

			// Wait for server's goroutines
			stubServer.Close()

			assert.Equal(tc.expectedCode, resp.Code)
			assert.Equal(tc.expectedAttempts, int(attemptCount.Load()), "Should have made exactly %d attempts", tc.expectedAttempts)
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

	// Written on the server's goroutine
	var gotTraceparent string
	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("traceparent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer stubServer.Close()

	// Use a plain http.Client (no otelhttp) - we only want to assert the
	// header survives the forwarder's hop-by-hop pruning.
	fwd := New(http.DefaultClient, stubServer.Listener.Addr().String(), SchemeHTTP, UpstreamInference, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, constants.OpenAIChatCompletionsEndpoint, nil)
	req.Header.Set("traceparent", inbound)
	resp := httptest.NewRecorder()
	fwd.Forward(resp, req, NoRequestMutation, PassthroughResponseMapper)

	// Wait for server's goroutines
	stubServer.Close()

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, inbound, gotTraceparent)
}

func TestForwardDeletesHopHeaders(t *testing.T) {
	assert := assert.New(t)

	// Written on the server's goroutine
	var got *http.Request
	stubServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r
	}))
	defer stubServer.Close()

	upstreamHost := stubServer.Listener.Addr().String()
	fwd := New(http.DefaultClient, upstreamHost, SchemeHTTP, UpstreamInference, slog.Default())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, constants.OpenAIChatCompletionsEndpoint, nil)
	req.Host = "endpoint.intranet.example.com"
	req.Header.Set("Connection", "Keep-Alive, X-Connection-Scoped")
	req.Header.Set("X-Connection-Scoped", "secret")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("Trailer", "X-Checksum")
	req.Header.Set("X-App-Header", "kept")

	resp := httptest.NewRecorder()
	fwd.Forward(resp, req, NoRequestMutation, PassthroughResponseMapper)

	// Wait for server's goroutines
	stubServer.Close()

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

func TestForwardReusesUpstreamConnection(t *testing.T) {
	testCases := map[string]struct {
		downstreamClose bool
	}{
		"keep-alive downstream":        {downstreamClose: false},
		"Connection: close downstream": {downstreamClose: true},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			var conns atomic.Int64
			stubServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, r.Proto)
			}))
			stubServer.EnableHTTP2 = true
			stubServer.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					conns.Add(1)
				}
			}
			stubServer.StartTLS()
			defer stubServer.Close()

			fwd := New(stubServer.Client(), stubServer.Listener.Addr().String(), SchemeHTTPS, UpstreamInference, slog.Default())

			for range 5 {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, constants.OpenAIChatCompletionsEndpoint, strings.NewReader("{}"))
				if tc.downstreamClose {
					req.Header.Set("Connection", "close")
					req.Close = true
				}
				resp := httptest.NewRecorder()
				fwd.Forward(resp, req, NoRequestMutation, PassthroughResponseMapper)

				assert.Equal(http.StatusOK, resp.Code)
				assert.Equal("HTTP/2.0", resp.Body.String())
			}
			assert.EqualValues(1, conns.Load())
		})
	}
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
	refused := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	tests := map[string]struct {
		transportErr error
		mutatorErr   error
		upstream     Upstream
		wantStatus   int
		wantMessage  string
		wantCode     string
		wantLog      string
	}{
		"connection failure -> 502 Bad Gateway": {
			transportErr: refused,
			upstream:     UpstreamInference,
			wantStatus:   http.StatusBadGateway,
			wantMessage:  "connection to the inference backend failed",
			wantCode:     "inference_connection_failed",
			wantLog:      `"upstream":"inference"`,
		},
		"timeout -> 504 Gateway Timeout": {
			transportErr: &net.OpError{Op: "dial", Err: timeoutError{}},
			upstream:     UpstreamModel,
			wantStatus:   http.StatusGatewayTimeout,
			wantMessage:  "connection to the model timed out",
			wantCode:     "model_connection_failed",
			wantLog:      `"upstream":"model"`,
		},
		"API shows the transport error": {
			transportErr: refused,
			upstream:     UpstreamAPI,
			wantStatus:   http.StatusBadGateway,
			wantMessage:  `connection to the Privatemode API failed: Post "https://upstream.invalid:443/v1/chat/completions": dial: connection refused`,
			wantCode:     "api_connection_failed",
			wantLog:      `"upstream":"api"`,
		},
		"model hides the upstream address": {
			transportErr: refused,
			upstream:     UpstreamModel,
			wantStatus:   http.StatusBadGateway,
			wantMessage:  "connection to the model failed",
			wantCode:     "model_connection_failed",
			wantLog:      `"upstream":"model"`,
		},
		"documents name the document service": {
			transportErr: refused,
			upstream:     UpstreamDocuments,
			wantStatus:   http.StatusBadGateway,
			wantMessage:  "connection to the document service failed",
			wantCode:     "document_connection_failed",
			wantLog:      `"upstream":"document"`,
		},
		"zero value names no upstream and hides the transport error": {
			transportErr: refused,
			wantStatus:   http.StatusBadGateway,
			wantMessage:  "connection to the upstream failed",
			wantCode:     "upstream_connection_failed",
			wantLog:      `"upstream":"upstream"`,
		},
		"mutator error wrapping a transport error is not a connection error": {
			mutatorErr:  fmt.Errorf("getting latest secret: %w", &url.Error{Op: "Get", URL: "https://secret.invalid", Err: refused}),
			upstream:    UpstreamAPI,
			wantStatus:  http.StatusInternalServerError,
			wantMessage: `forwarding request: mutating request: getting latest secret: Get "https://secret.invalid": dial: connection refused`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, tc.transportErr
			})}
			var logs bytes.Buffer
			fwd := New(client, "upstream.invalid:443", SchemeHTTPS, tc.upstream, slog.New(slog.NewJSONHandler(&logs, nil)))
			mutator := func(*http.Request) error { return tc.mutatorErr }

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, constants.OpenAIChatCompletionsEndpoint, nil)
			resp := httptest.NewRecorder()
			fwd.Forward(resp, req, mutator, PassthroughResponseMapper)

			var body openAIAPIErrorResponse
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			assert.Equal(tc.wantStatus, resp.Code)
			assert.Equal(tc.wantMessage, body.Error.Message)
			assert.Equal(tc.wantCode, body.Error.Code)
			if tc.wantLog != "" {
				assert.Contains(logs.String(), tc.wantLog)
			}
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
