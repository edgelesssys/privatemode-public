// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

/*
Package client implements a client to interact with Continuum's API.
*/
package client

import (
	"crypto/tls"
	"log/slog"
	"time"
)

// GetInsecureTLSConfig returns a TLS config that doesn't verify the secret-service certificate.
func GetInsecureTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // apigateway doesn't verify the secret svc
	}
}

// Client is used to interact with Continuum's API.
type Client struct {
	endpoint  string
	tlsConfig *tls.Config

	o *Opts
}

// New sets up a new [Client].
func New(endpoint string, tlsConfig *tls.Config, opts *Opts) *Client {
	opts = applyDefaultOpts(opts)

	c := &Client{
		endpoint:  endpoint,
		tlsConfig: tlsConfig,
		o:         opts,
	}
	return c
}

// SetTLSConfig sets the TLS config for the client.
func (c *Client) SetTLSConfig(tlsConfig *tls.Config) {
	c.tlsConfig = tlsConfig
}

// Opts contains options for a [Client].
type Opts struct {
	// Log is the logger to use.
	// Defaults to discarding all logs if not set.
	Log *slog.Logger
	// RetryInterval is the interval between retries.
	// Defaults to 5 seconds.
	RetryInterval time.Duration
	// MaxRetries is the maximum number of retries.
	// Defaults to 10.
	MaxRetries int
}

func applyDefaultOpts(o *Opts) *Opts {
	if o == nil {
		return &Opts{
			Log:           slog.New(slog.DiscardHandler),
			RetryInterval: 5 * time.Second,
			MaxRetries:    10,
		}
	}

	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.RetryInterval == 0 {
		o.RetryInterval = 5 * time.Second
	}
	if o.MaxRetries == 0 {
		o.MaxRetries = 10
	}

	return o
}
