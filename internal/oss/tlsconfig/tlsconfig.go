// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package tlsconfig provides TLS server configuration with certificate rotation.
package tlsconfig

import "crypto/tls"

// New validates the certificate and key files and returns a server configuration
// that reloads them for each full TLS handshake. Existing connections are unaffected.
// The files must contain a matching pair when read; loading errors fail the handshake.
func New(certPath, keyPath string) (*tls.Config, error) {
	getCert := func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		return &cert, err
	}
	if _, err := getCert(nil); err != nil {
		return nil, err
	}
	return &tls.Config{GetCertificate: getCert}, nil
}
