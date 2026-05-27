// Package httprpc provides a generic JSON-over-HTTP RPC handler.
//
// # Usage
//
// Using httprpc involves three pieces:
//
// RPC interface specification. The function name, request and response types, and any specific
// error codes. These should live in a package shared by server and client.
//
// Server-side implementation and routing. A Go function implementing the RPC, wrapped with
// [MakeHandler] and routed through an [net/http.ServeMux] at the RPC's name.
//
// Client-side caller. A typed wrapper around [ClientCall] on a [Client] that performs the call
// against the server's base URL.
//
// # Wire representation
//
// Transport is HTTP POST, with the RPC method name as the URL path (<BaseURL>/<name>). Bodies are
// JSON (Content-Type: application/json) and capped at 1 MiB on each side. Responses carry an
// X-Request-Id header.
//
// The request body is the JSON-encoded request value. The server parses strictly and rejects
// unknown fields. The client ignores unknown fields to allow schema evolution (older client with
// newer server).
//
// A successful response is HTTP 200 OK with the JSON-encoded response value. An error response is a
// non-2xx status with a JSON [ErrorResponse] body. The client should still process non-JSON bodies
// which may be produced by non-rpc-aware middleware (intermediate proxy, authn&authz, panic
// recoverer).
//
// Errors are classified as client faults (e.g. missing field, entity not found) or server faults
// (e.g. bug, dependency failure) via [ErrorResponse.ServerFault]. That means an RPC call completes
// with a status {ok, clientFaultError, serverFaultError}. The fault information is duplicated as
// HTTP 400 for client faults and HTTP 500 for server faults.
//
// # Error Codes
//
// Error codes in [ErrorResponse.Code] should be used by the caller for programmatic matching. Codes
// must be unique within a single RPC function, not globally. [SentinelError] is a simple way to
// define them.
package httprpc

import "mime"

// maxBodyBytes is the maximum allowed request body size (1 MiB).
const maxBodyBytes = 1 << 20

// ErrorResponse is the JSON error response sent by servers to clients.
type ErrorResponse struct {
	Message     string `json:"message"`
	Code        string `json:"code,omitzero"`
	ServerFault bool   `json:"server_fault,omitzero"`
}

// isJSONContentType reports whether ct is a JSON content type.
func isJSONContentType(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mt == "application/json"
}
