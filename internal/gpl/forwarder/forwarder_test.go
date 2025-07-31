package forwarder

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForwardStreaming(t *testing.T) {
	assert := assert.New(t)

	mutator := &stubMutator{
		mutateResponse: `"plainText"`,
	}
	responseMutator := WithFullJSONResponseMutation(mutator.mutate, nil, false)

	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		for range 20 {
			_, _ = w.Write([]byte("data: {\"field\": \"encryptedData\"}\n\n"))
			time.Sleep(5 * time.Millisecond)
		}
	}))
	defer stubServer.Close()

	forwarder := New("tcp", stubServer.Listener.Addr().String(), slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := httptest.NewRecorder()

	forwarder.Forward(
		resp,
		req,
		NoRequestMutation,
		responseMutator,
		NoHeaderMutation,
	)

	assert.Equal(http.StatusOK, resp.Code)
	assert.Equal("text/event-stream", resp.Header().Get("Content-Type"))
	var expectedResponse string
	for range 20 {
		expectedResponse += `data: {"field": "plainText"}` + "\n\n"
	}
	assert.Equal(expectedResponse, resp.Body.String())
}

func TestForwardStreamingAborted(t *testing.T) {
	assert := assert.New(t)

	mutator := &stubMutator{
		mutateResponse: `"plainText"`,
	}
	responseMutator := WithFullJSONResponseMutation(mutator.mutate, nil, false)

	sentFirstPart := make(chan struct{})

	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		// Send a complete chunk
		_, _ = w.Write([]byte("data: {\"field\": \"encryptedData\"}\n\n"))
		w.(http.Flusher).Flush()

		// Start sending a partial chunk
		_, _ = w.Write([]byte("data: {\"field\": "))
		w.(http.Flusher).Flush()

		// Wait before cancelling to let the client start reading
		time.Sleep(10 * time.Millisecond)
		sentFirstPart <- struct{}{}
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte("\"encryptedData\"}\n\n"))
	}))
	defer stubServer.Close()

	forwarder := New("tcp", stubServer.Listener.Addr().String(), slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	// Wrap req.Context() in a cancelable context
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)
	resp := httptest.NewRecorder()

	go func() {
		<-sentFirstPart
		cancel()
	}()

	forwarder.Forward(
		resp,
		req,
		NoRequestMutation,
		responseMutator,
		NoHeaderMutation,
	)

	assert.Equal(http.StatusOK, resp.Code)
	assert.Equal("text/event-stream", resp.Header().Get("Content-Type"))

	// Use bufio.Scanner to verify streaming response is chunked line by line
	scanner := bufio.NewScanner(resp.Body)
	chunkCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			fmt.Printf("chunk %d: %s\n", chunkCount, line)
			chunkCount++
		}
	}
	assert.NoError(scanner.Err())
	assert.Equal(1, chunkCount, "Should have received 1 complete chunk before abort")
}

func TestForwardNonStreaming(t *testing.T) {
	assert := assert.New(t)

	mutator := &stubMutator{
		mutateResponse: `"plainText"`,
	}
	responseMutator := WithFullJSONResponseMutation(mutator.mutate, nil, false)

	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var encryptedData string
		for range bufio.MaxScanTokenSize {
			encryptedData += "AB"
		}
		_, _ = w.Write(fmt.Appendf(nil, "{\"field\": \"%s\"}", encryptedData))
	}))
	defer stubServer.Close()

	forwarder := New("tcp", stubServer.Listener.Addr().String(), slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := httptest.NewRecorder()

	forwarder.Forward(
		resp,
		req,
		NoRequestMutation,
		responseMutator,
		NoHeaderMutation,
	)

	assert.Equal(http.StatusOK, resp.Code)
	assert.Equal("application/json", resp.Header().Get("Content-Type"))

	expectedResponse := `{"field": "plainText"}`
	assert.Equal(expectedResponse, resp.Body.String())
}

func TestForwardHeaderCopying(t *testing.T) {
	failingMutator := &stubMutator{
		mutateErr: assert.AnError,
	}
	responseMutator := WithFullJSONResponseMutation(failingMutator.mutate, nil, false)

	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"field": "AB"}`))
	}))
	defer stubServer.Close()

	forwarder := New("tcp", stubServer.Listener.Addr().String(), slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := httptest.NewRecorder()

	forwarder.Forward(
		resp,
		req,
		NoRequestMutation,
		responseMutator,
		NoHeaderMutation,
	)

	assert := assert.New(t)
	assert.Equal(http.StatusInternalServerError, resp.Code)
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
			expectedBody:        `{"error":{"message":"Internal error occurred","type":"","param":"","code":""}}`,
		},
		"SSE request": {
			acceptHeader:        "text/event-stream",
			code:                http.StatusBadRequest, // 400
			msg:                 "Invalid parameter",
			args:                nil,
			expectedStatusCode:  http.StatusBadRequest,
			expectedContentType: "text/event-stream",
			expectedBody:        "event: error\n\ndata: {\"error\":{\"message\":\"Invalid parameter\",\"type\":\"\",\"param\":\"\",\"code\":\"\"}}\n\n",
		},
		"Plain request with args": {
			acceptHeader:        "",                  // No accept header
			code:                http.StatusNotFound, // 404
			msg:                 "Resource %s not found",
			args:                []any{"item123"},
			expectedStatusCode:  http.StatusNotFound,
			expectedContentType: "application/json",
			expectedBody:        `{"error":{"message":"Resource item123 not found","type":"","param":"","code":""}}`,
		},
		"SSE request with args": {
			acceptHeader:        "text/event-stream",
			code:                http.StatusUnauthorized, // 401
			msg:                 "User %d unauthorized",
			args:                []any{42},
			expectedStatusCode:  http.StatusUnauthorized,
			expectedContentType: "text/event-stream",
			expectedBody:        "event: error\n\ndata: {\"error\":{\"message\":\"User 42 unauthorized\",\"type\":\"\",\"param\":\"\",\"code\":\"\"}}\n\n",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
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

func TestForwardRetry(t *testing.T) {
	testCases := map[string]struct {
		delay            time.Duration
		shouldRetry      bool
		expectedCode     int
		expectedAttempts int
	}{
		"no delay":   {0, true, http.StatusOK, 3},
		"with delay": {10 * time.Millisecond, true, http.StatusOK, 3},
		"no retry":   {0, false, http.StatusInternalServerError, 1},
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

			forwarder := New("tcp", stubServer.Listener.Addr().String(), slog.Default())

			retryCallback := func(statusCode int, body []byte, attempt int) (bool, time.Duration) {
				if !tc.shouldRetry {
					return false, 0
				}

				// Verify we get the correct response body for each attempt
				expectedBodies := []string{"Internal Server Error", "Still failing"}
				if attempt <= len(expectedBodies) {
					assert.Contains(string(body), expectedBodies[attempt-1], "Response body should match expected for attempt %d", attempt)
				}

				shouldRetry := statusCode == 500 && attempt < 3
				return shouldRetry, tc.delay
			}

			req := httptest.NewRequest(http.MethodPost, "/test", nil)
			resp := httptest.NewRecorder()

			startTime := time.Now()
			forwarder.ForwardWithRetry(
				resp,
				req,
				NoRequestMutation,
				NoResponseMutation{},
				NoHeaderMutation,
				retryCallback,
			)
			elapsed := time.Since(startTime)

			assert.Equal(tc.expectedCode, resp.Code)
			assert.Equal(tc.expectedAttempts, attemptCount, "Should have made exactly %d attempts", tc.expectedAttempts)

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
