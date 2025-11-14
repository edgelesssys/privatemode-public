// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package forwarder is used to forward http requests to a unix socket.
package forwarder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
)

const (
	// SchemeHTTPS protocol scheme.
	SchemeHTTPS ProtocolScheme = "https"
	// SchemeHTTP protocol scheme.
	SchemeHTTP ProtocolScheme = "http"

	// copyBufferSize is the buffer size used for copying the response body.
	// It is specifically chosen to be smaller than the default buffer used by Go,
	// to ensure streaming responses are comparatively smooth to directly interacting with the server.
	// Size was chosen through experimentation with vllm benchmarks.
	copyBufferSize = 1024 * 8
	// privateModeEncryptedHeader is the header used to indicate whether a response is encrypted.
	privateModeEncryptedHeader = "Privatemode-Encrypted"
)

// ProtocolScheme is the protocol scheme used for the forwarding.
type ProtocolScheme string

// ResponseMutator performs mutations on the given [io.Reader].
type ResponseMutator interface {
	Reader(reader io.Reader) io.Reader
	Mutate(body []byte) ([]byte, error)
}

// ErrorMessage represents an error response in the OpenAI API format for v1/chat/completions.
type ErrorMessage struct {
	Error APIError `json:"error"`
}

// APIError is the error for the OpenAI error format.
type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    string `json:"code"`
}

// Opts applies options to a [Forwarder.Forward] call.
type Opts func(*opts)

// WithRetryCallback applies the given RetryCallback to the forwarding call.
func WithRetryCallback(cb RetryCallback) Opts {
	return func(o *opts) {
		o.retryCallback = cb
	}
}

// WithHost forwards the request to the given host.
func WithHost(host string) Opts {
	return func(o *opts) {
		o.host = host
	}
}

// NoRequestMutation skips any mutation on the [*http.Request].
func NoRequestMutation(*http.Request) error { return nil }

// NoHeaderMutation skips any mutation on the [*http.Header].
func NoHeaderMutation(http.Header, http.Header) error { return nil }

// NoResponseMutation skips any mutation of the given [io.ReadCloser].
type NoResponseMutation struct{}

// Reader returns the given [io.Reader] without any mutation.
func (NoResponseMutation) Reader(rc io.Reader) io.Reader { return rc }

// Mutate returns the given byte slice without any mutation.
func (NoResponseMutation) Mutate(body []byte) ([]byte, error) { return body, nil }

// RetryCallback determines whether a request should be retried based on status code and attempt number.
// attempt is the number of requests made / the index of the next attempt, i.e., starting from 1.
// Returns (shouldRetry, delay) where shouldRetry indicates if retry should happen and delay is the backoff duration.
type RetryCallback func(statusCode int, errMsg string, attempt int) (bool, time.Duration)

// NoRetry is a RetryCallback that never retries.
var NoRetry RetryCallback

// Forwarder implements a simple http proxy to forward http requests over a unix socket.
type Forwarder struct {
	client         *http.Client
	log            *slog.Logger
	host           string
	protocolScheme ProtocolScheme
}

// New sets up a new forwarding proxy with a custom http client.
func New(client *http.Client, address string, scheme ProtocolScheme, log *slog.Logger) *Forwarder {
	return &Forwarder{
		client:         client,
		log:            log,
		host:           address,
		protocolScheme: scheme,
	}
}

// Forward mutates a request with the given mutator and forwards it to a different endpoint.
// It also mutates the response with the given responseMutator and responseHeaderMutator before
// writing it back to the client.
func (f *Forwarder) Forward(
	w http.ResponseWriter, req *http.Request,
	requestMutator RequestMutator, responseMutator ResponseMutator, responseHeaderMutator HeaderMutator,
	opts ...Opts,
) {
	options := defaultOpts(f)
	for _, opt := range opts {
		opt(options)
	}

	f.logInfo("Forwarding request", req)

	// Prepare request for forwarding to server
	req.RequestURI = ""
	delHopHeaders(req.Header)
	updateForwardedHeader(req.Header, req.RemoteAddr)

	// Not setting the host here leads to "no Host in request URL" errors.
	req.URL.Host = options.host
	// Not setting the scheme here leads to "http: no Host in request URL" errors.
	req.URL.Scheme = string(f.protocolScheme)

	resp, err := f.sendWithRetry(req, requestMutator, options.retryCallback)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			f.logWarning("Connection closed by client before request could be fully forwarded", err, req)
		} else {
			f.logError("Failed to forward request", err, req)
		}
		HTTPError(w, req, http.StatusInternalServerError, "forwarding request: %s", err)
		return
	}
	defer resp.Body.Close()

	// Sanitize response to forward to client
	delHopHeaders(resp.Header)

	// Allow caller to mutate headers.
	// This is necessary in cases where the caller wants to modify a header already present in the response.
	if err := responseHeaderMutator(resp.Header, req.Header); err != nil {
		f.logError("Failed to mutate header", err, req)
		HTTPError(w, req, http.StatusInternalServerError, "mutating header: %s", err)
		return
	}

	// Copy headers to response towards client.
	for headerName, headerValues := range resp.Header {
		// Skip Content-Length header since we might change the length in the following mutation.
		if headerName == "Content-Length" {
			continue
		}
		for _, headerValue := range headerValues {
			w.Header().Add(headerName, headerValue)
		}
	}

	if resp.Header.Get(privateModeEncryptedHeader) == "false" {
		responseMutator = NoResponseMutation{}
	}

	if strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
		// Wrap the ResponseWriter with a flushing writer to force immediate sending after each event.
		flusher, ok := w.(http.Flusher)
		if !ok {
			f.logError("ResponseWriter does not support flushing", nil, req)
			HTTPError(w, req, http.StatusInternalServerError, "ResponseWriter does not support flushing")
		}
		flushingWriter := &flushingWriter{w: w, flusher: flusher}

		// Copy headers before streaming the body, as this calls WriteHeader(200) otherwise.
		// No further calls to WriteHeader, e.g. through [HTTPError], may be made after this.
		w.WriteHeader(resp.StatusCode)

		// Write response to client using a small buffer to ensure smooth streaming.
		if _, err := io.CopyBuffer(flushingWriter, responseMutator.Reader(resp.Body), make([]byte, copyBufferSize)); err != nil {
			if errors.Is(err, context.Canceled) || req.Context().Err() == context.Canceled {
				f.logWarning("Connection closed by client before forwarding finished", err, req)
			} else {
				f.logError("Failed creating new body for forwarded message", err, req)
			}
			return
		}
	} else {
		// Read the entire response body, mutate it and write it to the client.
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			f.logError("Failed reading response body", err, req)
			HTTPError(w, req, http.StatusInternalServerError, "reading response body: %s", err)
			return
		}
		responseBody, err := responseMutator.Mutate(body)
		if err != nil {
			f.logError("Failed mutating response body", err, req)
			HTTPError(w, req, http.StatusInternalServerError, "mutating response body: %s", err)
			return
		}

		// Copy headers before writing the body, as this implicitly calls WriteHeader(200) otherwise.
		// No further calls to WriteHeader, e.g. through [HTTPError], may be made after this.
		w.WriteHeader(resp.StatusCode)

		if resp.StatusCode >= 400 {
			logger := f.log.Warn
			if resp.StatusCode >= 500 {
				logger = f.log.Error
			}
			f.logMsg(logger, "Forwarded request returned an error status code", nil, req, "statusCode", resp.StatusCode)
		}
		if _, err := w.Write(responseBody); err != nil {
			if errors.Is(err, context.Canceled) {
				f.logWarning("Connection closed by client before forwarding finished", err, req)
			} else {
				f.logError("Failed creating new body for forwarded message", err, req)
			}
			return
		}
	}

	f.log.Info("Forwarding finished successfully", "requestID", req.Header.Get(constants.RequestIDHeader), "responseStatus", resp.StatusCode)
}

// applyBackoffDelay applies the configured backoff delay before retrying.
func (f *Forwarder) applyBackoffDelay(ctx context.Context, delay time.Duration, attempt int, requestID string) error {
	if delay <= 0 {
		return nil
	}
	f.log.Debug("Retrying request after delay", "attempt", attempt, "delay", delay, "requestID", requestID)

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil // Delay completed
	case <-ctx.Done():
		return ctx.Err() // Request was cancelled
	}
}

// trySend attempts to send a request and returns whether to retry and any error.
func (f *Forwarder) trySend(req *http.Request, requestMutator RequestMutator, attempt int, retryCallback RetryCallback) (bool, *http.Response, error) {
	// Mutate request for this attempt
	if err := requestMutator(req); err != nil {
		return false, nil, fmt.Errorf("mutating request: %w", err)
	}

	requestID := req.Header.Get(constants.RequestIDHeader)

	resp, err := f.client.Do(req)
	if err != nil {
		shouldRetry, retryErr := f.shouldRetry(req.Context(), retryCallback, err.Error(), -1, requestID, attempt)
		return shouldRetry, nil, errors.Join(err, retryErr)
	}

	// Check if we should retry based on status code
	if resp.StatusCode < 400 {
		return false, resp, nil
	}

	// Read response body for error message (but restore it for further processing)
	bodyBytes, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		bodyBytes = fmt.Appendf(nil, "failed to read response body: %s", readErr)
	} else {
		resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	shouldRetry, retryErr := f.shouldRetry(req.Context(), retryCallback, string(bodyBytes), resp.StatusCode, requestID, attempt)
	return shouldRetry, resp, errors.Join(readErr, retryErr)
}

// sendWithRetry handles the retry logic for forwarding requests.
func (f *Forwarder) sendWithRetry(req *http.Request, requestMutator RequestMutator, retryCallback RetryCallback) (*http.Response, error) {
	// Shortcut if there is no retry configured to skip copying the body.
	if retryCallback == nil {
		_, resp, err := f.trySend(req, requestMutator, 1, retryCallback)
		return resp, err
	}

	// Read the request body once before the retry loop as the request will invalidate
	// the body and we have to redo the mutation as the content may change with each
	// retry (e.g., the encryption).
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("reading request body: %w", err)
	}
	req.Body.Close()

	// Forward request to inference server with retry logic
	attempt := 0

	for {
		attempt++

		// Create a fresh copy of the request body for each attempt
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		retry, resp, err := f.trySend(req, requestMutator, attempt, retryCallback)
		if retry {
			continue
		}
		return resp, err
	}
}

func (f *Forwarder) shouldRetry(
	ctx context.Context, retryCallback RetryCallback,
	errMsg string, statusCode int, requestID string, attempt int,
) (bool, error) {
	if retryCallback == nil {
		return false, nil
	}

	shouldRetry, delay := retryCallback(statusCode, errMsg, attempt)
	f.log.Warn("Request failed, checking retry conditions",
		"message", errMsg,
		"statusCode", statusCode,
		"attempt", attempt,
		"shouldRetry", shouldRetry,
		"delay", delay,
		"requestID", requestID)

	if !shouldRetry {
		return false, nil
	}

	if err := f.applyBackoffDelay(ctx, delay, attempt, requestID); err != nil {
		return false, fmt.Errorf("request cancelled during backoff: %w", err)
	}
	return true, nil
}

// HTTPError writes an error response to the client.
// Functions similarly to [http.Error], but also handles error reporting for SSE requests.
func HTTPError(w http.ResponseWriter, r *http.Request, code int, msg string, args ...any) {
	errObj := APIError{
		Message: fmt.Sprintf(msg, args...),
		Type:    "",
		Param:   "",
		Code:    "",
	}
	formattedMsgBytes, err := json.Marshal(ErrorMessage{Error: errObj})
	formattedMsg := string(formattedMsgBytes)
	if err != nil {
		// Only fall back to non-JSON error when we cannot even marshal the error (which is pretty bad)
		formattedMsg = fmt.Sprintf(msg, args...)
	}
	if expectedContentType := r.Header.Get("accept"); expectedContentType == "text/event-stream" {
		// If the client requested streaming we need to return the error correctly encoded.
		w.Header().Set("Content-Type", "text/event-stream")
		formattedMsg = fmt.Sprintf("event: error\n\ndata: %s\n\n", formattedMsg)
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set(privateModeEncryptedHeader, "false")
	w.WriteHeader(code)
	fmt.Fprint(w, formattedMsg)
}

func (f *Forwarder) logInfo(msg string, req *http.Request) {
	f.logMsg(f.log.Info, msg, nil, req)
}

func (f *Forwarder) logError(msg string, err error, req *http.Request) {
	f.logMsg(f.log.Error, msg, err, req)
}

func (f *Forwarder) logWarning(msg string, err error, req *http.Request) {
	f.logMsg(f.log.Warn, msg, err, req)
}

func (f *Forwarder) logMsg(logFn func(msg string, args ...any), msg string, err error, req *http.Request, extraArgs ...any) {
	args := []any{
		"remoteAddress", req.RemoteAddr,
		"method", req.Method,
		"path", req.URL.RequestURI(), // full url not available here, so we use RequestURI
		"requestID", req.Header.Get(constants.RequestIDHeader),
		"userAgent", req.UserAgent(),
		"clientVersion", req.Header.Get(constants.PrivatemodeVersionHeader),
		"clientOS", req.Header.Get(constants.PrivatemodeOSHeader),
		"clientArch", req.Header.Get(constants.PrivatemodeArchitectureHeader),
		"clientType", req.Header.Get(constants.PrivatemodeClientHeader),
		// shardKey can be very long (cache salt hash + potentially large content hash); truncate for logs.
		"shardKey", func() string {
			sh := req.Header.Get(constants.PrivatemodeShardKeyHeader)
			if len(sh) > constants.CacheSaltHashLength {
				return sh[:constants.CacheSaltHashLength] + "..."
			}
			return sh
		}(),
		"contentLength", req.ContentLength,
		"contentType", req.Header.Get("Content-Type"),
	}
	if err != nil {
		args = append(args, "error", err)
	}
	args = append(args, extraArgs...)
	logFn(msg, args...)
}

// delHopHeaders deletes hop-by-hop headers which should not be forwarded.
// See the HTTP RFC for more details: https://datatracker.ietf.org/doc/html/rfc9110#name-message-forwarding
func delHopHeaders(header http.Header) {
	hopHeaders := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailers",
		"Transfer-Encoding",
		"Upgrade",
	}
	for _, h := range hopHeaders {
		header.Del(h)
	}
}

// updateForwardedHeader updates the X-Forwarded-For header with the client's IP address.
func updateForwardedHeader(header http.Header, remoteAddr string) {
	if clientIP, _, err := net.SplitHostPort(remoteAddr); err == nil {
		if prior, ok := header["X-Forwarded-For"]; ok {
			clientIP = strings.Join(prior, ", ") + ", " + clientIP
		}
		header.Set("X-Forwarded-For", clientIP)
	}
}

type opts struct {
	host          string
	retryCallback RetryCallback
}

func defaultOpts(fw *Forwarder) *opts {
	return &opts{
		host:          fw.host,
		retryCallback: NoRetry,
	}
}

// flushingWriter wraps an http.ResponseWriter and flushes after each write.
// This ensures that SSE events are sent immediately without buffering.
type flushingWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (fw *flushingWriter) Write(p []byte) (n int, err error) {
	n, err = fw.w.Write(p)
	if err != nil {
		return n, err
	}
	fw.flusher.Flush()
	return n, nil
}
