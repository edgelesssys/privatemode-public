// Package httputil provides HTTP utilities.
package httputil

import (
	"crypto/tls"
	"net/http"
)

// Clone the default transport on package init to ensure it's unmodified. Panic on type failure is desired.
var transport = http.DefaultTransport.(*http.Transport).Clone() //nolint:forcetypeassert

// NewTransport creates a Transport with safe defaults.
func NewTransport() *http.Transport {
	return transport.Clone()
}

// InsecureNewSkipVerifyClient creates a Client that skips TLS verification.
func InsecureNewSkipVerifyClient() *http.Client {
	transport := NewTransport()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &http.Client{Transport: transport}
}
