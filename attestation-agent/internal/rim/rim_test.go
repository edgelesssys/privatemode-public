//go:build integration

package rim

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntegrationFetchRIM(t *testing.T) {
	require := require.New(t)

	client := New(&http.Client{}, "https://rim.attestation.nvidia.com/", slog.Default())

	identity, err := client.FetchRIM(t.Context(), "NV_GPU_DRIVER_GH100_580.105.08")
	require.NoError(err)
	require.NotNil(identity)
}
