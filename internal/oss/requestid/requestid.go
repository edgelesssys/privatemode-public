// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package requestid provides utilities for handling request IDs in our infrastructure.
package requestid

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const (
	// Header is the header used to identify requests inside our infrastructure.
	// It is generated based on UUIDv7 in the apigateway.
	// User set values (e.g. provided by the pm-proxy) are overwritten, but logged.
	Header = "X-Request-ID"
	// UserHeader is a user supplied identifier for requests.
	// It is generated based on UUIDv7 by client applications, such as the privatemode-proxy or SDKs, and logged by the apigateway.
	UserHeader = "Privatemode-User-Request-ID"

	// Unknown is the default value when the request ID is not set.
	Unknown = "unknown"
)

type contextKey string

const requestIDKey contextKey = "requestID"

// New generates a new request ID based on UUIDv7.
func New() string {
	return uuid.Must(uuid.NewV7()).String()
}

// FromContext returns the request ID saved in the given [context.Context].
// Returned values are sanitized for logging.
// Returns [Unknown] if no request ID is found in the context.
func FromContext(ctx context.Context) (string, bool) {
	if id, ok := ctx.Value(requestIDKey).(string); ok && id != "" {
		return sanitizeString(id), true
	}
	return Unknown, false
}

// WithContext saves the given request ID in the provided [context.Context].
func WithContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// FromHeader returns the request ID for a given request from [Header].
// Returned values are sanitized for logging.
// Returns [Unknown] if no request ID is found in the request header.
func FromHeader(header http.Header) string {
	return fromHeaderWithDefault(header, Header)
}

// FromUserHeader returns the user supplied request ID for a given request from [UserHeader].
// Returned values are sanitized for logging.
// Returns [Unknown] if no user request ID is found in the request header.
func FromUserHeader(header http.Header) string {
	return fromHeaderWithDefault(header, UserHeader)
}

func fromHeaderWithDefault(header http.Header, key string) string {
	if id := header.Get(key); id != "" {
		return sanitizeString(id)
	}
	return Unknown
}

// sanitizeString truncates the input string to 64 characters
// and replaces any non-alphanumeric characters (except for '-', '_' and '.') with '?'.
func sanitizeString(s string) string {
	if len(s) > 64 {
		s = s[:64] + "..."
	}
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '?'
	}, s)
	return s
}
