// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package forwarder

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is returned by forwarding middleware functions.
// It wraps an underlying error and the HTTP status code that should be returned to the client.
type Error struct {
	err        error
	msg        string
	StatusCode int
}

// NewError creates a new [Error] with the given message, underlying error, and HTTP status code.
// msg may be left empty to simply wrap the error with a status code.
func NewError(msg string, err error, statusCode int) *Error {
	return &Error{
		msg:        msg,
		err:        err,
		StatusCode: statusCode,
	}
}

// Error prints the error message.
func (e *Error) Error() string {
	if e.msg != "" {
		return fmt.Sprintf("%s: %s", e.msg, e.err.Error())
	}
	return e.err.Error()
}

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error {
	return e.err
}

// StatusFromError returns the HTTP status code embedded in the given error,
// by unwrapping it to [Error].
// Returns [http.StatusOK] if the error is nil, or [http.StatusInternalServerError]
// for errors of a different type.
func StatusFromError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if fwErr, ok := errors.AsType[*Error](err); ok {
		return fwErr.StatusCode
	}
	return http.StatusInternalServerError
}
