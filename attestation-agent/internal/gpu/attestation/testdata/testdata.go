package testdata

import (
	_ "embed"
)

// JWKS is a JSON Web Key Set, containing the key used to sign the test tokens.
//
//go:embed jwks.json
var JWKS []byte

// SigningKey is the PEM-encoded testing (private) key used to sign the test tokens.
// FOR TEST-USE ONLY.
//
//go:embed testkey.pem
var SigningKeyPEM []byte
