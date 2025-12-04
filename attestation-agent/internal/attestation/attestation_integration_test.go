//go:build gpu

package attestation

import (
	"crypto/rand"
	"log/slog"
	"os"
	"testing"

	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu"
	"github.com/stretchr/testify/require"
)

func TestIssueVerify(t *testing.T) {
	require := require.New(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	gpuClient, err := gpu.NewClient(logger)
	require.NoError(err)

	devices, err := gpuClient.ListGPUs()
	require.NoError(err)
	require.NotEmpty(devices)

	// Test attestation on just a single GPU
	issuer := NewIssuer(devices[0], logger)
	nonce := [32]byte{}
	n, err := rand.Read(nonce[:])
	require.NoError(err)
	require.Equal(32, n)

	report, _, err := issuer.Issue(nonce)
	require.NoError(err)

	_, err = ParseReport(report)
	require.NoError(err)
}
