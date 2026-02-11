// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package secretexchange provides utilities for secret exchange.
package secretexchange

import (
	"crypto/sha256"
	"encoding/base64"
)

// Hash computes a hash over the public key from the request and the encapsulated key from the response.
func Hash(reqPublicKey, respEncapKey []byte) []byte {
	hash := sha256.New()
	hash.Write(reqPublicKey)
	hash.Write(respEncapKey)
	return hash.Sum(nil)
}

// ID derives the secret ID from the request public key.
func ID(reqPublicKey []byte) string {
	hash := sha256.Sum256(reqPublicKey)
	return base64.StdEncoding.EncodeToString(hash[:])
}
