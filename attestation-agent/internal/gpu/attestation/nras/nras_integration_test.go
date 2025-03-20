//go:build integration

package nras

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJWKS(t *testing.T) {
	require := require.New(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	nrasClient := NewClient(logger)

	jwks, err := nrasClient.JWKS(t.Context())
	require.NoError(err)
	require.NotEmpty(jwks)
}
