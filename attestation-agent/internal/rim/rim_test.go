//go:build integration

package rim

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu"
	"github.com/stretchr/testify/require"
)

func TestIntegrationFetchRIM(t *testing.T) {
	require := require.New(t)

	client := New(&http.Client{}, "https://rim.attestation.nvidia.com/", slog.Default())

	identity, err := client.FetchDriverRIM(t.Context(), gpu.ArchHopper, "", "580.105.08")
	require.NoError(err)
	require.NotNil(identity)
}
