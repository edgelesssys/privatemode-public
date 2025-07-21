// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package client

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// retryAPICall executes a given function multiple times and aborts if the grpc connection fails terminally.
func retryAPICall(
	ctx context.Context, do func(context.Context) error,
	retryInterval time.Duration, maxAttempts int,
) (retErr error) {
	for range maxAttempts {
		retErr = do(ctx)
		if retErr == nil {
			return nil
		}

		// Abort early if we receive a handshake error, or any non "Unavailable" grpc error
		if status, ok := status.FromError(retErr); ok && (status.Code() != codes.Unavailable || strings.Contains(status.Message(), "transport: authentication handshake failed")) {
			return retErr
		}

		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return retErr
}
