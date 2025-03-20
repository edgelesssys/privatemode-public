//go:build gpu
// +build gpu

package attestation

import (
	"crypto/rand"
	"log/slog"
	"os"
	"testing"

	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu"
	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu/attestation/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueVerify(t *testing.T) {
	h100Defaults := &policy.NvidiaHopper{
		Debug:                   false,
		SecureBoot:              true,
		EATVersion:              "EAT-21",
		MismatchingMeasurements: []int{},
		DriverVersions:          []string{"535.104.05", "535.129.03", "550.90.07"},
		VBIOSVersions:           []string{"96.00.74.00.1C", "96.00.9F.00.04"},
	}

	require := require.New(t)
	assert := assert.New(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	gpuClient, err := gpu.NewClient(logger)
	require.NoError(err)

	verifier := NewVerifier(h100Defaults, logger)

	devices, err := gpuClient.ListGPUs()
	require.NoError(err)
	require.NotEmpty(devices)

	// Test attestation on just a single GPU
	issuer := NewIssuer(devices[0], logger)
	nonce := [32]byte{}
	n, err := rand.Read(nonce[:])
	require.NoError(err)
	require.Equal(32, n)

	eat, err := issuer.Issue(t.Context(), nonce)
	require.NoError(err)

	err = verifier.Verify(t.Context(), eat, nonce)
	assert.NoError(err)
}
