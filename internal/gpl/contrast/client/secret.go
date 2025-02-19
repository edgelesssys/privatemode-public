// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package client

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/edgelesssys/continuum/internal/gpl/grpc/dialer"
	"github.com/edgelesssys/continuum/internal/gpl/proto/secret-service/userapi"
)

// SetSecrets uploads secrets to Continuum's Secret Service.
// A zero or negative TTL will create secrets without expiration.
func (c *Client) SetSecrets(ctx context.Context, secrets map[string][]byte, ttl time.Duration) error {
	conn, err := dialer.New(&net.Dialer{}, c.tlsConfig).NewConn(c.endpoint)
	if err != nil {
		return fmt.Errorf("failed to dial Secret Service endpoint %q: %w", c.endpoint, err)
	}
	defer conn.Close()
	client := userapi.NewUserAPIClient(conn)

	if err := retryAPICall(ctx, func(ctx context.Context) error {
		_, err = client.SetSecrets(ctx, &userapi.SetSecretsRequest{Secrets: secrets, TimeToLive: int64(ttl.Seconds())})
		if err != nil {
			return fmt.Errorf("set secrets method: %w", err)
		}
		return nil
	}, c.o.RetryInterval, c.o.MaxRetries); err != nil {
		return err
	}
	return nil
}
