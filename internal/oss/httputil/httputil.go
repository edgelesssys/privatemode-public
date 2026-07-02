// Package httputil provides HTTP utilities.
package httputil

import (
	"crypto/tls"
	"net/http"
	"time"
)

var transport = func() *http.Transport {
	// Panic on type failure is desired during init.
	tr := http.DefaultTransport.(*http.Transport).Clone() //nolint:forcetypeassert
	// Eventually detect broken HTTP/2 connections.
	tr.HTTP2 = &http.HTTP2Config{SendPingTimeout: 5 * time.Second}
	return tr
}()

// NewTransport creates a Transport with safe defaults.
func NewTransport() *http.Transport {
	return transport.Clone()
}

// NewClient creates a Client with safe defaults.
func NewClient() *http.Client {
	return &http.Client{Transport: NewTransport()}
}

// InsecureNewSkipVerifyClient creates a Client that skips TLS verification.
func InsecureNewSkipVerifyClient() *http.Client {
	transport := NewTransport()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &http.Client{Transport: transport}
}
