//go:build integration

package rim

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchRIM(t *testing.T) {
	require := require.New(t)

	client := New("https://rim.attestation.nvidia.com/", slog.Default())

	identity, err := client.FetchRIM(t.Context(), "NV_GPU_DRIVER_GH100_535.104.05")
	require.NoError(err)
	require.NotNil(identity)
}
