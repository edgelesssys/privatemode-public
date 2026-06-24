// Package mtls provides utilities for applying mTLS.
package mtls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// Identity is a workload's mTLS identity for cluster-internal communication. the cert/key it presents (as both
// server and client) and the CA pool it trusts for peer verification.
type Identity interface {
	// ServerConfig returns a fresh [*tls.Config] for serving with mutual auth. Cert/key are
	// reloaded from disk on each handshake.
	ServerConfig() *tls.Config

	// ClientConfig returns a fresh [*tls.Config] for dialing peers with mutual auth. Cert/key are
	// reloaded from disk on each handshake.
	ClientConfig() *tls.Config
}

// LoadIdentity reads the bundle (tls.crt, tls.key, ca.crt) from the given paths and returns an
// Identity. Cert/key are reloaded on each handshake. The CA bundle is loaded once.
func LoadIdentity(certPath, keyPath, caPath string) (Identity, error) {
	id := &identity{certPath: certPath, keyPath: keyPath}
	if _, err := id.loadKeyPair(); err != nil {
		return nil, fmt.Errorf("loading identity cert/key: %w", err)
	}
	var err error
	id.caPool, err = loadCA(caPath)
	if err != nil {
		return nil, fmt.Errorf("loading identity CA: %w", err)
	}
	return id, nil
}

// ClientConfigWithoutCert returns a [*tls.Config] for dialing peers with server auth.
func ClientConfigWithoutCert(caPath string) (*tls.Config, error) {
	caPool, err := loadCA(caPath)
	if err != nil {
		return nil, fmt.Errorf("loading CA: %w", err)
	}
	return &tls.Config{RootCAs: caPool}, nil
}

func loadCA(caPath string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("reading CA: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parsing CA: no PEM certs found in %s", caPath)
	}
	return caPool, nil
}

type identity struct {
	certPath string
	keyPath  string
	caPool   *x509.CertPool
}

func (i *identity) loadKeyPair() (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(i.certPath, i.keyPath)
	return &cert, err
}

func (i *identity) ServerConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return i.loadKeyPair()
		},
		ClientCAs:  i.caPool.Clone(),
		ClientAuth: tls.RequireAndVerifyClientCert,
	}
}

func (i *identity) ClientConfig() *tls.Config {
	return &tls.Config{
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return i.loadKeyPair()
		},
		RootCAs: i.caPool.Clone(),
	}
}
