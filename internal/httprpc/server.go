package httprpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/internal/oss/requestid"
)

var (
	// ErrInvalidRequest is returned when the request body cannot be decoded, e.g. malformed JSON,
	// unknown fields, or extra trailing data.
	ErrInvalidRequest = NewSentinelError("invalid request", "httprpc.InvalidRequest")
	// ErrMethodNotAllowed is returned when the HTTP method is not POST.
	ErrMethodNotAllowed = NewSentinelError("method not allowed", "httprpc.MethodNotAllowed")
	// ErrUnsupportedMediaType is returned when the Content-Type is not application/json.
	ErrUnsupportedMediaType = NewSentinelError("content type must be application/json", "httprpc.UnsupportedMediaType")
	// ErrRequestEntityTooLarge is returned when the request body exceeds the allowed size.
	ErrRequestEntityTooLarge = NewSentinelError("request body too large", "httprpc.RequestEntityTooLarge")
	// ErrReadBody is returned when reading the request body fails due to lower-level reasons.
	ErrReadBody = NewSentinelError("failed reading request body", "httprpc.ReadBody")
	// ErrWriteResponse is returned when writing the response body fails due to lower-level reasons.
	ErrWriteResponse = NewSentinelError("failed writing response body", "httprpc.WriteResponse")
	// ErrInternalError is returned for unexpected server failures, including errors from RPC
	// functions that are not [*FuncError].
	ErrInternalError = NewSentinelServerError("internal server error", "httprpc.Internal")
)

// FuncError is returned by RPC functions to produce an error response.
type FuncError struct {
	// ClientMessage is the human-readable message returned to the caller.
	ClientMessage string

	// Code identifies the error class for programmatic matching by callers. May be empty.
	Code string

	// ServerFault marks who is responsible: false when caused by the caller (e.g. missing field,
	// entity not found), true when the error is caused by the server (e.g. bug, dependency failure).
	ServerFault bool

	// Err is the underlying cause, kept for server-side logging and [errors.Is] / [errors.As].
	Err error
}

func (f *FuncError) Error() string {
	prefix := ""
	if f.Code != "" {
		prefix = fmt.Sprintf("[%s] ", f.Code)
	}

	if f.Err != nil {
		return fmt.Sprintf("%s%s: %v", prefix, f.ClientMessage, f.Err)
	}
	return prefix + f.ClientMessage
}

func (f *FuncError) Unwrap() error {
	return f.Err
}

// Is implements the errors.Is interface for matching error codes.
func (f *FuncError) Is(target error) bool {
	if f.Code == "" {
		return false
	}

	if matcher, ok := target.(ErrorCodeMatcher); ok {
		return matcher.MatchesErrorCode(f.Code)
	}

	return false
}

func (f *FuncError) toErrorResponse() ErrorResponse {
	return ErrorResponse{
		Message:     f.ClientMessage,
		Code:        f.Code,
		ServerFault: f.ServerFault,
	}
}

// HandlerOption applies an option to a handler created by [MakeHandler].
type HandlerOption func(*handlerOpts)

// handlerOpts holds the options of a handler created by [MakeHandler].
type handlerOpts struct {
	accessLogLevel slog.Level
}

// WithAccessLogLevel sets the level of the per-call "Served RPC request" line, which defaults to
// [slog.LevelInfo].
func WithAccessLogLevel(level slog.Level) HandlerOption {
	return func(o *handlerOpts) {
		o.accessLogLevel = level
	}
}

// MakeHandler returns an [http.HandlerFunc] that decodes a JSON request of type Req, calls fn, and
// encodes the response of type Resp as JSON.
//
// If fn returns a [*FuncError], it is used in the error response. [SentinelError] is a simple way
// to return client-matchable errors.
func MakeHandler[Req any, Resp any](
	log *slog.Logger, fn func(ctx context.Context, req Req) (Resp, error), options ...HandlerOption,
) http.HandlerFunc {
	opts := handlerOpts{accessLogLevel: slog.LevelInfo}
	for _, apply := range options {
		apply(&opts)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		requestID := requestid.New()
		w.Header().Set(requestid.Header, requestID)
		ctx := requestid.WithContext(r.Context(), requestID)
		reqLog := log.With("path", r.URL.Path)

		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeErrorRespAndLog(ctx, reqLog, opts, w, http.StatusMethodNotAllowed, ErrMethodNotAllowed.toErrorResponse())
			return
		}

		ct := r.Header.Get("Content-Type")
		if !isJSONContentType(ct) {
			writeErrorRespAndLog(ctx, reqLog, opts, w, http.StatusUnsupportedMediaType, ErrUnsupportedMediaType.toErrorResponse())
			return
		}

		body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
		defer body.Close()

		buf, err := io.ReadAll(body)
		if err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				writeErrorRespAndLog(ctx, reqLog, opts, w, http.StatusRequestEntityTooLarge, ErrRequestEntityTooLarge.toErrorResponse())
				return
			}
			writeErrorRespAndLog(ctx, reqLog, opts, w, http.StatusBadRequest, ErrReadBody.toErrorResponse())
			return
		}

		var req Req
		dec := json.NewDecoder(bytes.NewReader(buf))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeErrorRespAndLog(ctx, reqLog, opts, w, http.StatusBadRequest, ErrInvalidRequest.toErrorResponse())
			return
		}

		// Reject requests that send more than one JSON value.
		if dec.More() {
			writeErrorRespAndLog(ctx, reqLog, opts, w, http.StatusBadRequest, ErrInvalidRequest.toErrorResponse())
			return
		}

		resp, err := fn(ctx, req)
		if err != nil {
			if funcErr, ok := errors.AsType[*FuncError](err); ok {
				var status int
				if funcErr.ServerFault {
					status = http.StatusInternalServerError
					reqLog.ErrorContext(ctx, "RPC func returned server fault", "error", err, "errorCode", funcErr.Code)
				} else {
					status = http.StatusBadRequest
					reqLog.DebugContext(ctx, "RPC func returned client fault", "error", err, "errorCode", funcErr.Code)
				}
				writeErrorRespAndLog(ctx, reqLog, opts, w, status, funcErr.toErrorResponse())
				return
			}
			reqLog.ErrorContext(ctx, "RPC func returned unhandled error", "error", err)
			writeErrorRespAndLog(ctx, reqLog, opts, w, http.StatusInternalServerError, ErrInternalError.toErrorResponse())
			return
		}

		writeSuccessRespAndLog(ctx, reqLog, opts, w, &resp)
	}
}

func writeSuccessRespAndLog(ctx context.Context, log *slog.Logger, opts handlerOpts, w http.ResponseWriter, resp any) {
	respBuf, err := json.Marshal(resp)
	if err != nil {
		log.ErrorContext(ctx, "Marshaling response failed", "error", err)
		writeErrorRespAndLog(ctx, log, opts, w, http.StatusInternalServerError, ErrInternalError.toErrorResponse())
		return
	}
	if !writeJSON(ctx, log, opts, w, http.StatusOK, respBuf) {
		return
	}
	log.Log(
		ctx, opts.accessLogLevel, "Served RPC request", "outcome", "success", "rpcStatus", "ok",
		"httpStatus", http.StatusOK,
	)
}

func writeErrorRespAndLog(ctx context.Context, log *slog.Logger, opts handlerOpts, w http.ResponseWriter, httpStatus int, resp ErrorResponse) {
	respBuf, err := json.Marshal(&resp)
	if err != nil {
		log.ErrorContext(ctx, "Marshaling error response failed", "error", err)
		respBuf = []byte(`{"message":"internal error","code":"httprpc.Internal"}`)
		httpStatus = http.StatusInternalServerError
	}
	if !writeJSON(ctx, log, opts, w, httpStatus, respBuf) {
		return
	}
	outcome, rpcStatus := "success", "clientFaultError"
	if resp.ServerFault {
		outcome, rpcStatus = "failure", "serverFaultError"
	}
	log.Log(
		ctx, opts.accessLogLevel, "Served RPC request", "outcome", outcome, "rpcStatus", rpcStatus,
		"httpStatus", httpStatus, "errorCode", resp.Code,
	)
}

func writeJSON(ctx context.Context, log *slog.Logger, opts handlerOpts, w http.ResponseWriter, status int, body []byte) bool {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		log.DebugContext(ctx, "Writing response failed", "error", err)
		log.Log(ctx, opts.accessLogLevel, "Served RPC request", "outcome", "failure", "errorCode", ErrWriteResponse.code)
		return false
	}
	return true
}
