// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package forwarder is used to forward HTTP requests to another server.
//
// A forwarder is a proxy which receives input requests from a downstream client and sends
// a derived request to an upstream server. Similarly, a downstream response is derived from the
// upstream response and sent back to the downstream client.
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
	"net/textproto"
	"strings"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/persist"
	"github.com/edgelesssys/continuum/internal/oss/requestid"
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
)

// ProtocolScheme is the protocol scheme used for the forwarding.
type ProtocolScheme string

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

// WithMaxBodyBytes limits the request body to n bytes.
// Requests with a body exceeding this limit receive a 413 response with msg included. An empty
// msg string leads to a default message.
func WithMaxBodyBytes(n int64, msg string) Opts {
	return func(o *opts) {
		o.maxBodyBytes = n
		if msg != "" {
			o.maxBodyExceededMsg = msg
		}
	}
}

// NoRequestMutation skips any mutation on the [*http.Request].
func NoRequestMutation(*http.Request) error { return nil }

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

// Forward forwards a downstream request req to an upstream and relays the response back to the downstream through w.
// It applies the mutators and mappers, which are translating input to output request and response.
// The upstream address is controlled with [New] or [Opts]. Retry behaviour is controlled with [Opts].
func (f *Forwarder) Forward(
	w http.ResponseWriter, req *http.Request,
	requestMutator RequestMutator, responseMapper ResponseMapper,
	opts ...Opts,
) {
	options := defaultOpts(f)
	for _, opt := range opts {
		opt(options)
	}

	ctx := req.Context()
	log := f.log.With(
		"remoteAddress", req.RemoteAddr,
		"method", req.Method,
		"path", req.URL.RequestURI(), // full url not available here, so we use RequestURI
	)

	log.InfoContext(ctx, "Forwarding request")

	// Clone a base request reused across forwarding attempts. Also enforces body size limits.
	baseReq, ok := f.cloneIncomingRequest(w, req, options, log)
	if !ok {
		return
	}

	// Prepare request for forwarding to upstream server
	baseReq.RequestURI = ""
	delHopHeaders(baseReq.Header)

	// Not setting the host here leads to "no Host in request URL" errors.
	baseReq.URL.Host = options.host
	// Not setting the scheme here leads to "http: no Host in request URL" errors.
	baseReq.URL.Scheme = string(f.protocolScheme)
	// Clear Host header. Will be derived from baseReq.URL.Host by [http.Client].
	baseReq.Host = ""

	resp, err := f.sendWithRetry(baseReq, requestMutator, options.retryCallback)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			log.WarnContext(
				ctx,
				"Downstream request cancelled (connection lost) before request could be fully forwarded",
				"error", err,
			)
			HTTPError(w, req, http.StatusInternalServerError, "forwarding request: %s", err)
			return
		}
		// Upstream failures report as gateway errors (502/504); 500 is for proxy-internal faults.
		status := http.StatusInternalServerError
		var upErr *transportError
		if errors.As(err, &upErr) {
			status = upErr.status
		}
		log.ErrorContext(ctx, "Failed to forward request", "error", err, "status", status)
		HTTPError(w, req, status, "forwarding request: %s", err)
		return
	}
	// Response body closing happens below, dependent on the mapper.

	if upstreamRequestID := requestid.FromHeader(resp.Header); upstreamRequestID != requestid.Unknown {
		log = log.With("upstreamRequestID", upstreamRequestID)
	}

	// Produce the downstream response from the upstream response.
	dsResp, err := responseMapper(resp)
	if err != nil {
		_ = resp.Body.Close()
		log.ErrorContext(ctx, "Failed to map upstream response to downstream response", "error", err)
		HTTPError(w, req, http.StatusInternalServerError, "mapping response: %s", err)
		return
	}
	switch r := dsResp.(type) {
	case *StreamingResponse:
		// Wrapped body must be closed after sending, cascades down to the http.Response
		defer r.Body.Close()
	case *UnaryResponse:
		// Body already fully read: Close right away
		resp.Body.Close()
	default:
		// Unexpected type! Close http.Response after sending at least
		log.WarnContext(ctx, fmt.Sprintf("Unexpected type of mapped response %T", dsResp))
		defer resp.Body.Close()
	}

	if err := SendResponse(w, dsResp); err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() == context.Canceled {
			log.WarnContext(
				ctx,
				"Downstream request cancelled (connection lost) while sending response",
				"error", err,
			)
			return
		}
		// The status and headers are already committed, so we cannot turn this into a gateway
		// error. Abort the connection instead of letting net/http terminate the chunked stream
		// cleanly, so the client sees a failed rather than a complete response.
		log.ErrorContext(ctx, "Failed sending response to downstream client, aborting connection", "error", err)
		logging.AddSummaryAttrs(ctx, "aborted", true)
		panic(http.ErrAbortHandler)
	}

	// Log error status codes
	statusCode := dsResp.GetStatusCode()
	if statusCode := statusCode; statusCode >= 400 {
		level := slog.LevelWarn
		if statusCode >= 500 {
			level = slog.LevelError
		}
		log.Log(ctx, level, "Upstream returned an error status code", "statusCode", statusCode)
	}

	log.InfoContext(
		ctx,
		"Forwarding finished successfully",
		"responseStatus", statusCode,
	)
}

// applyBackoffDelay applies the configured backoff delay before retrying.
func (f *Forwarder) applyBackoffDelay(ctx context.Context, delay time.Duration, attempt int) error {
	if delay <= 0 {
		return nil
	}
	f.log.DebugContext(ctx, "Retrying request after delay", "attempt", attempt, "delay", delay)

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil // Delay completed
	case <-ctx.Done():
		return ctx.Err() // Request was cancelled
	}
}

// cloneIncomingRequest clones req and persists its body to memory.
// It enforces [opts.maxBodyBytes] if > 0. For any error, it responds with a HTTP error and returns ok=false.
func (f *Forwarder) cloneIncomingRequest(w http.ResponseWriter, req *http.Request, options *opts, log *slog.Logger) (baseReq *http.Request, ok bool) {
	ctx := req.Context()
	var err error
	if options.maxBodyBytes > 0 {
		baseReq, err = persist.CloneRequest(w, req, options.maxBodyBytes)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				log.WarnContext(ctx, "Request body too large", "error", err)
				HTTPError(w, req, http.StatusRequestEntityTooLarge, "%s", options.maxBodyExceededMsg)
			} else {
				log.ErrorContext(ctx, "Failed to read request body", "error", err)
				HTTPError(w, req, http.StatusInternalServerError, "reading request body: %s", err)
			}
			return nil, false
		}
	} else {
		baseReq, err = persist.CloneRequestUnlimited(req)
		if err != nil {
			log.ErrorContext(ctx, "Failed to read request body", "error", err)
			HTTPError(w, req, http.StatusInternalServerError, "reading request body: %s", err)
			return nil, false
		}
	}
	return baseReq, true
}

// trySend attempts to send a request and returns whether to retry and any error.
func (f *Forwarder) trySend(req *http.Request, requestMutator RequestMutator, attempt int, retryCallback RetryCallback) (bool, *http.Response, error) {
	// Mutate request for this attempt
	if err := requestMutator(req); err != nil {
		return false, nil, fmt.Errorf("mutating request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		// Transport error contacting the upstream: report as a gateway error, not a 500.
		upErr := newTransportError(err)
		// No upstream status is available; pass -1 to the retry callback.
		shouldRetry, retryErr := f.shouldRetry(req.Context(), retryCallback, err.Error(), -1, attempt)
		return shouldRetry, nil, errors.Join(upErr, retryErr)
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

	shouldRetry, retryErr := f.shouldRetry(req.Context(), retryCallback, string(bodyBytes), resp.StatusCode, attempt)
	return shouldRetry, resp, errors.Join(readErr, retryErr)
}

// transportError wraps a failure contacting the upstream, carrying the gateway status
// (504 on timeout, else 502) to report downstream instead of a 500.
type transportError struct {
	status int
	err    error
}

func newTransportError(err error) *transportError {
	status := http.StatusBadGateway
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		status = http.StatusGatewayTimeout
	}
	return &transportError{status: status, err: err}
}

func (e *transportError) Error() string { return e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

// sendWithRetry handles the retry logic for forwarding requests.
func (f *Forwarder) sendWithRetry(req *http.Request, requestMutator RequestMutator, retryCallback RetryCallback) (*http.Response, error) {
	// Shortcut if there is no retry configured to skip cloning the request.
	if retryCallback == nil {
		_, resp, err := f.trySend(req, requestMutator, 1, retryCallback)
		return resp, err
	}

	// Forward request to inference server with retry logic.
	attempt := 0

	for {
		attempt++

		// The req has already been read into memory by Forwarder, so Unlimited is fine here.
		reqCopy, err := persist.CloneRequestUnlimited(req)
		if err != nil {
			return nil, fmt.Errorf("cloning request: %w", err)
		}

		retry, resp, err := f.trySend(reqCopy, requestMutator, attempt, retryCallback)
		if retry {
			continue
		}
		return resp, err
	}
}

func (f *Forwarder) shouldRetry(
	ctx context.Context, retryCallback RetryCallback,
	errMsg string, statusCode int, attempt int,
) (bool, error) {
	if retryCallback == nil {
		return false, nil
	}

	shouldRetry, delay := retryCallback(statusCode, errMsg, attempt)
	f.log.WarnContext(ctx, "Request failed, checking retry conditions",
		"message", errMsg,
		"statusCode", statusCode,
		"attempt", attempt,
		"shouldRetry", shouldRetry,
		"delay", delay)

	if !shouldRetry {
		return false, nil
	}

	if err := f.applyBackoffDelay(ctx, delay, attempt); err != nil {
		return false, fmt.Errorf("request cancelled during backoff: %w", err)
	}
	return true, nil
}

// openAIAPIErrorResponse represents the error response returned by the /v1/chat/completions endpoint.
// It is a copy internal/oss/openai.APIErrorResponse for serialization only.
type openAIAPIErrorResponse struct {
	Error openAIAPIError `json:"error"`
}

// openAIAPIError contains error details.
// It is a copy of internal/oss/openai.APIError for serialization only.
type openAIAPIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitzero"`
	Code    string `json:"code,omitzero"`
}

// HTTPError writes an error response to the client.
// Functions similarly to [http.Error], but also handles error reporting for SSE requests.
func HTTPError(w http.ResponseWriter, r *http.Request, code int, msg string, args ...any) {
	errObj := openAIAPIError{
		Message: fmt.Sprintf(msg, args...),
		Type:    "",
		Param:   "",
		Code:    "",
	}
	formattedMsgBytes, err := json.Marshal(openAIAPIErrorResponse{Error: errObj})
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
	w.WriteHeader(code)
	fmt.Fprint(w, formattedMsg)
}

// delHopHeaders deletes hop-by-hop headers which should not be forwarded. It mirrors the behaviour
// of [net/http/httputil.ReverseProxy]
// See the HTTP RFC for more details: https://datatracker.ietf.org/doc/html/rfc9110#name-message-forwarding
func delHopHeaders(header http.Header) {
	// Remove headers listed in the "Connection" header.
	for _, f := range header["Connection"] {
		for sf := range strings.SplitSeq(f, ",") {
			if sf = textproto.TrimString(sf); sf != "" {
				header.Del(sf)
			}
		}
	}

	hopHeaders := []string{
		"Connection",
		"Proxy-Connection", // non-standard but still sent by some clients
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",      // canonicalized version of "TE"
		"Trailer", // not "Trailers" per https://www.rfc-editor.org/errata_search.php?eid=4522
		"Transfer-Encoding",
		"Upgrade",
	}
	for _, h := range hopHeaders {
		header.Del(h)
	}
}

type opts struct {
	host               string
	retryCallback      RetryCallback
	maxBodyBytes       int64
	maxBodyExceededMsg string
}

func defaultOpts(fw *Forwarder) *opts {
	return &opts{
		host:               fw.host,
		retryCallback:      NoRetry,
		maxBodyExceededMsg: "request body too large",
	}
}

// extractFlusher extracts the [http.Flusher] for w by traversing the Unwrap chain, like
// [http.ResponseController].
func extractFlusher(w http.ResponseWriter) (http.Flusher, bool) {
	for {
		if f, ok := w.(http.Flusher); ok {
			return f, true
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil, false
		}
		w = u.Unwrap()
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

// UnaryResponse is a basic representation of a unary response with its body fully in memory.
type UnaryResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// responseSeal is the seal of the [Response] interface.
func (u *UnaryResponse) responseSeal() {}

// GetStatusCode fulfills [Response.GetStatusCode].
func (u *UnaryResponse) GetStatusCode() int { return u.StatusCode }

// GetHeader fulfills [Response.GetHeader].
func (u *UnaryResponse) GetHeader() http.Header { return u.Header }

var _ Response = (*UnaryResponse)(nil)

// ReadUnaryResponse reads the upstream response body into memory and copies the status code.
// The body is limited to maxBytes via [http.MaxBytesReader]. Headers are initialized empty.
func ReadUnaryResponse(resp *http.Response, maxBytes int64) (*UnaryResponse, error) {
	// http.MaxBytesReader: passing nil for the ResponseWriter is explicitly supported though not documented
	body, err := io.ReadAll(http.MaxBytesReader(nil, resp.Body, maxBytes))
	if err != nil {
		return nil, err
	}

	return &UnaryResponse{
		StatusCode: resp.StatusCode,
		Header:     http.Header{},
		Body:       body,
	}, nil
}

// cloneForwardableHeaders clones headers with hop-by-hop and Content-Length headers removed.
// Content-Length is excluded because body mutation may change the length.
func cloneForwardableHeaders(h http.Header) http.Header {
	out := h.Clone()
	delHopHeaders(out)
	out.Del("Content-Length")
	return out
}

// StreamingResponse is a basic representation of a streaming response with the body as an
// [io.ReadCloser]. Streaming means that the body is sent in chunks to the client as it is
// generated through Read. Omit the content-length header for that to work properly. See
// [SendResponse] for details on the flushing behaviour. The body may be read exactly once for
// sending out the response and must be closed in all cases. To transform the body, wrap Body
// with a new [io.ReadCloser] whose Close cascades to the wrapped reader, and assign the wrapper.
type StreamingResponse struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
}

// responseSeal is the seal of the [Response] interface.
func (s *StreamingResponse) responseSeal() {}

// GetStatusCode fulfills [Response.GetStatusCode].
func (s *StreamingResponse) GetStatusCode() int { return s.StatusCode }

// GetHeader fulfills [Response.GetHeader].
func (s *StreamingResponse) GetHeader() http.Header { return s.Header }

var _ Response = (*StreamingResponse)(nil)

// NewStreamingResponse wraps the upstream response body as a streaming response and copies the
// status code. Headers are initialized empty.
func NewStreamingResponse(resp *http.Response) *StreamingResponse {
	return &StreamingResponse{
		StatusCode: resp.StatusCode,
		Header:     http.Header{},
		Body:       resp.Body,
	}
}

// NewStreamingResponseWithHeaders is like [NewStreamingResponse] but also copies forwardable
// headers. Prefer [NewStreamingResponse] for new code.
func NewStreamingResponseWithHeaders(resp *http.Response) *StreamingResponse {
	return &StreamingResponse{
		StatusCode: resp.StatusCode,
		Header:     cloneForwardableHeaders(resp.Header),
		Body:       resp.Body,
	}
}

// Response is the sum of all response types.
// Type assert to extract [UnaryResponse] or [StreamingResponse]. In addition, interface methods
// allow extracting common fields.
type Response interface {
	// GetStatusCode returns the HTTP status code.
	GetStatusCode() int
	// GetHeader returns the HTTP headers. The returned map is mutable and shared with the
	// underlying response.
	GetHeader() http.Header

	// responseSeal seals the interface: all implementations are in this package.
	responseSeal()
}

// ResponseMapper takes the upstream response and produces the downstream response to send out.
// The upstream response is treated as read-only input and the returned [Response] must be an
// independent value through deep-copying all input fields.
//
// The caller closes resp.Body in all paths; for a [StreamingResponse], ownership transfers to the
// returned wrapper, whose Close must cascade to resp.Body.
//
// The mapper may apply a pipeline of mutators, where each mutator addresses a single concern.
// Mutators modify the response in place. Example:
//
//	func(resp *http.Response) (Response, error) {
//	    // Create initial downstream response
//	    dr, err := ReadUnaryResponse(resp, maxBytes)
//	    if err != nil {
//	        return nil, err
//	    }
//
//	    // Apply a pipeline of mutators, each addressing a single concern
//
//	    if err := decryptBody(dr); err != nil {
//	        return nil, err
//	    }
//
//	    setContentType("application/json", dr)
//
//	    // Return the final downstream response
//	    return dr, nil
//	}
//
// Mutators do not have to follow a fixed signature but should take a pointer to the response along
// with additional parameters and may return an error.
type ResponseMapper func(*http.Response) (Response, error)

// SendResponse sends out the resp to the downstream client via w.
// For [StreamingResponse], it expects w to support flushing, and flushes after each Write() on w.
// Because [io.Copy] performs one Write() for each Read(), resp can control flushing.
// Alternatively, if resp implements [io.WriterTo], it can call Write() appropriately itself.
func SendResponse(w http.ResponseWriter, resp Response) error {
	if resp == nil {
		return errors.New("nil response")
	}
	switch r := resp.(type) {
	case *UnaryResponse:
		writeHeaderTo(w.Header(), r.Header)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(r.Body)))
		w.WriteHeader(r.StatusCode)
		if _, err := w.Write(r.Body); err != nil {
			return fmt.Errorf("writing response body: %w", err)
		}
	case *StreamingResponse:
		flusher, ok := extractFlusher(w)
		if !ok {
			return errors.New("ResponseWriter does not support flushing")
		}
		writeHeaderTo(w.Header(), r.Header)
		w.WriteHeader(r.StatusCode)
		fw := &flushingWriter{w: w, flusher: flusher}
		copyBuffer := make([]byte, copyBufferSize)
		if _, err := io.CopyBuffer(fw, r.Body, copyBuffer); err != nil {
			return fmt.Errorf("streaming response body: %w", err)
		}
	default:
		return fmt.Errorf("unsupported response type %T", resp)
	}
	return nil
}

func writeHeaderTo(dst, src http.Header) {
	for k, vs := range src {
		// No cloning here: The header is written out immediately after this call, so any changes
		// after are trailers on distinct keys.
		dst[k] = vs
	}
}
