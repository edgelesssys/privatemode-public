// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package auth provides functions for authenticating API requests.
package auth

import (
	"fmt"
	"net/http"
	"strings"

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
