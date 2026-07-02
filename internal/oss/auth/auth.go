// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package auth provides functions for authenticating API requests.
package auth

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"golang.org/x/crypto/bcrypt"
)

const (
	// Bearer indicates an authorization header with 'Bearer <API_KEY>'.
	Bearer Type = "Bearer"
	// Token indicates an authorization header with a 'Token <token>'.
	Token Type = "Token"
)

// Type of the authorization scheme.
type Type string

// Credential is an authentication credential applied to outgoing Privatemode API
// requests. It abstracts over the different authentication methods the API gateway
// accepts (API key, Clerk JWT, or anonymous).
//
// A non-initialized ("zero") Credential applies no authentication.
type Credential struct {
	header string
	value  string
}

// APIKey returns a Credential that authenticates with a Privatemode API key via the
// Authorization header ('Bearer <key>').
func APIKey(key string) Credential {
	return Credential{header: "Authorization", value: string(Bearer) + " " + key}
}

// JWT returns a Credential that authenticates with a Clerk session JWT via the
// Privatemode-JWT header.
func JWT(token string) Credential {
	return Credential{header: constants.PrivatemodeJWTHeader, value: token}
}

// Anonymous returns a Credential for anonymous authentication. The fingerprint carries
// client-collected data via the Privatemode-Anonymous-Auth header and is used by the
// gateway for abuse control only.
func Anonymous(fingerprint string) Credential {
	return Credential{header: constants.PrivatemodeAnonymousAuthHeader, value: fingerprint}
}

// Apply sets the credential's header on h. It is a no-op for the zero Credential.
func (c Credential) Apply(h http.Header) {
	if c.IsZero() {
		return
	}
	h.Set(c.header, c.value)
}

// IsZero reports whether c is the zero Credential, carrying no authentication.
func (c Credential) IsZero() bool {
	return c.header == ""
}

// Error is returned when no valid Authorization header is found in a request.
type Error struct {
	authKey Type
}

func newError(authKey Type) *Error {
	return &Error{authKey: authKey}
}

// Error returns the error message.
func (e *Error) Error() string {
	return fmt.Sprintf("no auth found: expected Authorization header with '%s <auth>'", e.authKey)
}

// GetAuth extracts the credentials for a given auth scheme from the Authorization header.
func GetAuth(auth Type, header http.Header) (string, error) {
	authorization := strings.Split(header.Get("Authorization"), " ")
	if len(authorization) != 2 || authorization[0] != string(auth) {
		return "", newError(auth)
	}
	return authorization[1], nil
}

// ValidateAuthToken compares the given API token with the valid token hash.
func ValidateAuthToken(authToken string, validAuthTokenHash string) error {
	return bcrypt.CompareHashAndPassword([]byte(validAuthTokenHash), []byte(authToken))
}
