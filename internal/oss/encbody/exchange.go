// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package encbody implements a full-body encryption (FBE) wire format and protocol for HTTP
// requests/responses.
//
// Users should initialize a per-exchange (HTTP request + response) encryption context via
// [ClientExchange] or [ServerExchange].
//
// For background, see RFC-28 "Full Body Encryption and Usage Reports" and RFC-29 "Online
// Authenticated Encryption Module".
package encbody

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"

	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/oae"
	"github.com/edgelesssys/continuum/internal/oss/persist"
)

const (
	// RequestOAEHeader is the HTTP header containing the encoded OAE request stream header.
	RequestOAEHeader = "Privatemode-OAE-Request-Header"
	// ResponseOAEHeader is the HTTP header containing the encoded OAE response stream header.
	ResponseOAEHeader = "Privatemode-OAE-Response-Header"
	// ContentType is the Content-Type of a full-body-encrypted request or response.
	ContentType = "application/vnd.privatemode.encrypted-body"
)

var (
	// ErrAlreadyStarted is returned when a request or response is started a second time.
	ErrAlreadyStarted = errors.New("exchange direction already started")

	// ErrNotEncrypted is returned when an encrypted body is required but the Content-Type does not
	// indicate one.
	ErrNotEncrypted = errors.New("not an encrypted body")
)

// IsEncrypted reports whether h declares an encrypted body in its Content-Type.
func IsEncrypted(h http.Header) bool {
	mediaType, _, err := mime.ParseMediaType(h.Get("Content-Type"))
	return err == nil && mediaType == ContentType
}

func verifyIsEncrypted(h http.Header) error {
	if !IsEncrypted(h) {
		return fmt.Errorf(
			"%w: Content-Type %q is not %q",
			ErrNotEncrypted, h.Get("Content-Type"), ContentType,
		)
	}
	return nil
}

// encodeOAEHeader encodes raw as an RFC 9651 byte sequence (:base64:).
func encodeOAEHeader(raw []byte) string {
	return ":" + base64.StdEncoding.EncodeToString(raw) + ":"
}

// decodeOAEHeader parses a byte sequence value produced by [encodeOAEHeader].
func decodeOAEHeader(value string) ([]byte, error) {
	if value == "" {
		return nil, fmt.Errorf("missing OAE header")
	}
	if len(value) < 2 || value[0] != ':' || value[len(value)-1] != ':' {
		return nil, fmt.Errorf("malformed OAE header: missing colon delimiters")
	}
	raw, err := base64.StdEncoding.DecodeString(value[1 : len(value)-1])
	if err != nil {
		return nil, fmt.Errorf("decoding OAE header: %w", err)
	}
	return raw, nil
}

// ClientExchange encrypts a request and decrypts its response. It is single-use.
type ClientExchange struct {
	sealer       *oae.Sealer
	pending      *oae.PendingOpener
	reqOAEHeader []byte
	reqStarted   bool
	respStarted  bool
}

// InitializeClientExchange initializes a requester-side exchange secured by secret.
func InitializeClientExchange(secret []byte) (*ClientExchange, error) {
	sealer, pending, reqHeader, err := oae.InitializeRequester(secret)
	if err != nil {
		return nil, fmt.Errorf("initializing OAE requester: %w", err)
	}
	return &ClientExchange{
		sealer:       sealer,
		pending:      pending,
		reqOAEHeader: reqHeader,
	}, nil
}

// EncryptRequest encrypts the body of r in place. The full body is read into memory, callers must
// bound the body size (e.g. via [http.MaxBytesReader]). HTTP headers are preserved.
// authenticatedHeaders is carried inside the encrypted stream, separate from HTTP headers.
// Its keys are case-sensitive metadata names.
func (e *ClientExchange) EncryptRequest(r *http.Request, authenticatedHeaders map[string]string) error {
	if e.reqStarted {
		return ErrAlreadyStarted
	}
	e.reqStarted = true
	body, err := persist.ReadBodyUnlimited(r)
	if err != nil {
		return fmt.Errorf("reading body: %w", err)
	}
	appHeader := appHeader{ContentType: r.Header.Get("Content-Type"), Headers: authenticatedHeaders}
	encrypted, err := encryptWhole(body, e.sealer, appHeader)
	if err != nil {
		return fmt.Errorf("encrypting body: %w", err)
	}
	persist.SetBody(r, encrypted)
	r.Header.Set("Content-Type", ContentType)
	r.Header.Set(RequestOAEHeader, encodeOAEHeader(e.reqOAEHeader))
	return nil
}

// DecryptResponse decrypts r and returns a streaming response with forwardable headers copied from
// r. Returns [ErrNotEncrypted] if r is not encrypted. maxFrameSize bounds each encrypted frame.
// The caller must close the returned response on success or the input r on error.
func (e *ClientExchange) DecryptResponse(r *http.Response, maxFrameSize uint32) (*forwarder.StreamingResponse, error) {
	if err := verifyIsEncrypted(r.Header); err != nil {
		return nil, err
	}
	if e.respStarted {
		return nil, ErrAlreadyStarted
	}
	e.respStarted = true
	dsResp := forwarder.NewStreamingResponseWithHeaders(r)

	respHeader, err := decodeOAEHeader(dsResp.Header.Get(ResponseOAEHeader))
	if err != nil {
		return nil, fmt.Errorf("extracting OAE header: %w", err)
	}
	opener, err := e.pending.Accept(respHeader)
	if err != nil {
		return nil, fmt.Errorf("initializing OAE opener: %w", err)
	}
	dec, appHeader, err := openDecrypter(dsResp.Body, opener, maxFrameSize)
	if err != nil {
		return nil, fmt.Errorf("initializing decrypter: %w", err)
	}
	dsResp.Body = dec

	if appHeader.ContentType != "" {
		dsResp.Header.Set("Content-Type", appHeader.ContentType)
	} else {
		dsResp.Header.Del("Content-Type")
	}
	dsResp.Header.Del(ResponseOAEHeader)

	return dsResp, nil
}

// ServerExchange decrypts a request and encrypts its response. It is single-use.
type ServerExchange struct {
	opener      *oae.Opener
	sealer      *oae.Sealer
	respHeader  []byte
	reqStarted  bool
	respStarted bool
}

// InitializeServerExchange initializes a responder-side exchange secured by secret, using the OAE
// request header carried on r. It returns [ErrNotEncrypted] if r is not an encrypted request.
func InitializeServerExchange(secret []byte, r *http.Request) (*ServerExchange, error) {
	if err := verifyIsEncrypted(r.Header); err != nil {
		return nil, err
	}
	reqHeader, err := decodeOAEHeader(r.Header.Get(RequestOAEHeader))
	if err != nil {
		return nil, fmt.Errorf("extracting OAE header: %w", err)
	}
	opener, sealer, respHeader, err := oae.InitializeResponder(secret, reqHeader)
	if err != nil {
		return nil, fmt.Errorf("initializing OAE responder: %w", err)
	}
	return &ServerExchange{
		opener:     opener,
		sealer:     sealer,
		respHeader: respHeader,
	}, nil
}

// DecryptRequest decrypts the body of r in place. On error r is left in an undefined state. The
// full body is read into memory, callers must bound the body size (e.g. via
// [http.MaxBytesReader]). HTTP headers are preserved. Authenticated metadata is returned
// separately, only after the complete request has been successfully decrypted.
func (e *ServerExchange) DecryptRequest(r *http.Request) (map[string]string, error) {
	if e.reqStarted {
		return nil, ErrAlreadyStarted
	}
	e.reqStarted = true
	dec, appHeader, err := openDecrypter(r.Body, e.opener, math.MaxUint32)
	if err != nil {
		return nil, fmt.Errorf("initializing decrypter: %w", err)
	}
	plaintext, err := io.ReadAll(dec)
	if err != nil {
		_ = dec.Close()
		return nil, fmt.Errorf("decrypting request: %w", err)
	}
	if err := dec.Close(); err != nil {
		return nil, fmt.Errorf("closing decrypter: %w", err)
	}

	persist.SetBody(r, plaintext)
	if appHeader.ContentType != "" {
		r.Header.Set("Content-Type", appHeader.ContentType)
	} else {
		r.Header.Del("Content-Type")
	}
	r.Header.Del(RequestOAEHeader)
	return appHeader.Headers, nil
}

// EncryptUnaryResponse encrypts a buffered response body as a complete encrypted body. Headers are
// preserved.
func (e *ServerExchange) EncryptUnaryResponse(resp *forwarder.UnaryResponse) error {
	if e.respStarted {
		return ErrAlreadyStarted
	}
	e.respStarted = true
	appHeader := appHeader{ContentType: resp.Header.Get("Content-Type")}
	body, err := encryptWhole(resp.Body, e.sealer, appHeader)
	if err != nil {
		return fmt.Errorf("encrypting body: %w", err)
	}

	resp.Body = body

	resp.Header.Set("Content-Type", ContentType)
	resp.Header.Set(ResponseOAEHeader, encodeOAEHeader(e.respHeader))

	return nil
}

// EncryptSSEResponse wraps a streaming SSE response body as an encrypted body. Headers are
// preserved.
func (e *ServerExchange) EncryptSSEResponse(resp *forwarder.StreamingResponse, maxEventBytes int) error {
	if e.respStarted {
		return ErrAlreadyStarted
	}
	e.respStarted = true
	appHeader := appHeader{ContentType: resp.Header.Get("Content-Type")}
	body, err := openSSEEncrypter(resp.Body, e.sealer, appHeader, maxEventBytes)
	if err != nil {
		return fmt.Errorf("initializing SSE encrypter: %w", err)
	}
	resp.Body = body

	resp.Header.Set("Content-Type", ContentType)
	resp.Header.Set(ResponseOAEHeader, encodeOAEHeader(e.respHeader))

	return nil
}
