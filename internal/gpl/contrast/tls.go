// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// Package contrast contains any Contrast specific configuration.
package contrast

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
)

// ServerTLSConfig returns a TLS config for the Contrast server.
func ServerTLSConfig(meshCertPath string) (*tls.Config, error) {
	if meshCertPath == "" {
		meshCertPath = "/contrast/tls-config"
	}
	caCerts := x509.NewCertPool()
	meshCert, err := os.ReadFile(filepath.Join(meshCertPath, "mesh-ca.pem"))
	if err != nil {
		return nil, err
	}
	if !caCerts.AppendCertsFromPEM(meshCert) {
		return nil, errors.New("failed to append mesh certificate to pool")
	}
	cert, err := tls.LoadX509KeyPair(filepath.Join(meshCertPath, "certChain.pem"), filepath.Join(meshCertPath, "key.pem"))
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caCerts,
	}
	return tlsConfig, nil
}

// ClientTLSConfigFromDir returns a TLS config for the Contrast client from the cert directory created by the Contrast Initializer.
func ClientTLSConfigFromDir(certPath string) (*tls.Config, error) {
	if certPath == "" {
		certPath = "/contrast/tls-config"
	}
	meshCert, err := os.ReadFile(filepath.Join(certPath, "mesh-ca.pem"))
	if err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(filepath.Join(certPath, "certChain.pem"), filepath.Join(certPath, "key.pem"))
	if err != nil {
		return nil, err
	}
	return ClientTLSConfig(meshCert, []tls.Certificate{cert})
}

// ClientTLSConfig returns a TLS config for the Contrast client.
func ClientTLSConfig(meshCert []byte, clientCerts []tls.Certificate) (*tls.Config, error) {
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(meshCert) {
		return nil, errors.New("failed to append mesh certificate to pool")
	}
	tlsConfig := &tls.Config{
		RootCAs:      rootCAs,
		Certificates: clientCerts,
	}
	return tlsConfig, nil
}
