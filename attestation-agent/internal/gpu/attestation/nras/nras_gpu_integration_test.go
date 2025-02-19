//go:build gpu

package nras

import (
	"context"
	"crypto/rand"
	"log/slog"
	"os"
	"testing"

	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttestGPU(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	nrasClient := NewClient(logger)

	gpuClient, err := gpu.NewClient(logger)
	require.NoError(err)

	devices, err := gpuClient.ListGPUs()
	require.NoError(err)
	require.NotEmpty(devices)

	d := devices[0]

	nonce := [32]byte{}
	n, err := rand.Read(nonce[:])
	require.NoError(err)
	require.Equal(32, n)

	report, err := d.AttestationReport(nonce)
	require.NoError(err)

	cert, err := d.Certificate()
	require.NoError(err)

	eat, err := nrasClient.AttestGPU(
		context.Background(),
		ArchHopper,
		nonce,
		report,
		cert,
	)
	assert.NoError(err)
	assert.NotEmpty(eat)
}
