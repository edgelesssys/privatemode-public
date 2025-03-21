// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// package forwarder is used to forward http requests to a unix socket.
package forwarder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
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

// NoResponseMutation skips any mutation of the given [io.ReadCloser].
func NoResponseMutation(rc io.Reader) io.Reader { return rc }

// NoRequestMutation skips any mutation on the [*http.Request].
func NoRequestMutation(*http.Request) error { return nil }

// NoHeaderMutation skips any mutation on the [*http.Header].
func NoHeaderMutation(http.Header, http.Header) error { return nil }

// ResponseMutator performs mutations on the given [io.Reader].
type ResponseMutator func(reader io.Reader) io.Reader

// RequestMutator mutates an [*http.Request].
type RequestMutator func(request *http.Request) error

// HeaderMutator mutates a [http.Header].
// response is the header object of the response. Writes should usually go here.
// request is the header object of the request.
type HeaderMutator func(response http.Header, request http.Header) error

// Forwarder implements a simple http proxy to forward http requests over a unix socket.
type Forwarder struct {
	client         *http.Client
	log            *slog.Logger
	host           string
	protocolScheme ProtocolScheme
}

// New sets up a new http forwarding proxy.
func New(network, address string, log *slog.Logger) *Forwarder {
	host := address
	if network == "unix" {
		host = "unix"
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial(network, address)
			},
		},
	}
	return NewWithClient(client, host, SchemeHTTP, log)
}

// NewWithClient sets up a new forwarding proxy with a custom http client.
func NewWithClient(client *http.Client, address string, scheme ProtocolScheme, log *slog.Logger) *Forwarder {
	return &Forwarder{
		client:         client,
		log:            log,
		host:           address,
		protocolScheme: scheme,
	}
}

// Forward a requests to a different endpoint.
func (f *Forwarder) Forward(
	w http.ResponseWriter, req *http.Request,
	requestMutator RequestMutator, responseMutator ResponseMutator, headerMutator HeaderMutator,
) {
	f.log.Info("Forwarding request", "remoteAddress", req.RemoteAddr, "method", req.Method, "url", req.URL.String())

	// Prepare request for forwarding to server
	req.RequestURI = ""
	delHopHeaders(req.Header)
	updateForwardedHeader(req.Header, req.RemoteAddr)

	// Not setting the host here leads to "no Host in request URL" errors.
	req.URL.Host = f.host
	// Not setting the scheme here leads to "http: no Host in request URL" errors.
	req.URL.Scheme = string(f.protocolScheme)

	// Mutate request
	if err := requestMutator(req); err != nil {
		f.log.Error("Failed to mutate request", "error", err)
		HTTPError(w, req, http.StatusInternalServerError, "mutating request: %s", err)
		return
	}

	// Forward request to inference server
	resp, err := f.client.Do(req)
	if err != nil {
		f.log.Error("Failed to forward request", "error", err)
		HTTPError(w, req, http.StatusInternalServerError, "forwarding request: %s", err)
		return
	}
	defer resp.Body.Close()

	// Sanitize response to forward to client
	delHopHeaders(resp.Header)

	// Allow caller to mutate headers.
	// This is necessary in cases where the caller wants to modify a header already present in the response.
	if err := headerMutator(resp.Header, req.Header); err != nil {
		f.log.Error("Failed to mutate header", "error", err)
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

	w.WriteHeader(resp.StatusCode)

	// Write response to client using a small buffer to ensure smooth streaming.
	if _, err := io.CopyBuffer(w, responseMutator(resp.Body), make([]byte, copyBufferSize)); err != nil {
		if errors.Is(err, context.Canceled) {
			f.log.Warn("Connection closed by client before forwarding finished", "error", err)
		} else {
			f.log.Error("Failed creating new body for forwarded message", "error", err)
		}
		return
	}
	f.log.Info("Forwarding finished successfully")
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

// HTTPError writes an error response to the client.
// Functions similarly to [http.Error], but also handles error reporting for SSE requests.
func HTTPError(w http.ResponseWriter, r *http.Request, code int, msg string, args ...any) {
	msg = fmt.Sprintf(msg, args...)
	if expectedContentType := r.Header.Get("accept"); expectedContentType == "text/event-stream" {
		// If the client requested streaming we need to return the error correctly encoded.
		w.Header().Set("Content-Type", "text/event-stream")
		msg = fmt.Sprintf("event: error\n\ndata: %s\n\n", msg)
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	fmt.Fprintln(w, msg)
}
