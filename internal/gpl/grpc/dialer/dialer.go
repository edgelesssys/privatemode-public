// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// package dialer provides a grpc dialer that can be used to create grpc client connections.
package dialer

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials"
)

// Dialer can open grpc client connections with TLS.
type Dialer struct {
	netDialer NetDialer
	tlsConfig *tls.Config
}

// New creates a new Dialer without aTLS.
func New(netDialer NetDialer, tlsConfig *tls.Config) *Dialer {
	return &Dialer{
		netDialer: netDialer,
		tlsConfig: tlsConfig,
	}
}

// NewConn creates a new grpc client connection to the given target using TLS.
func (d *Dialer) NewConn(target string) (*grpc.ClientConn, error) {
	return grpc.NewClient(target,
		// Use custom aTLS credentials to secure this connection
		grpc.WithTransportCredentials(credentials.NewTLS(d.tlsConfig)),
		grpc.WithConnectParams(grpc.ConnectParams{
			// We need a high initial timeout, because otherwise the client will get stuck in a reconnect loop
			// where the timeout is too low to get a full handshake done.
			MinConnectTimeout: 30 * time.Second,
			Backoff:           backoff.DefaultConfig,
		}),
	)
}

// NetDialer implements the net Dialer interface.
type NetDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}
