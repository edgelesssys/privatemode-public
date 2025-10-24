//go:build gpu

package gpu

import (
	"log/slog"
	"os"
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

/*
NOTE: As the test runs are ephemeral, we don't actively
close the GPU client (i.e. unmap the library) after each
test. As the process terminates, this should not be a problem.
*/

// TestListGPUs tests the listing of GPUs through NVML.
// It requires libnvidia-ml.so.1 to be present on the system.
func TestListGPUs(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client, err := NewClient(logger)
	require.NoError(err)

	gpus, err := client.ListGPUs()
	assert.NoError(err)
	t.Log(gpus)
}

// TestCCFeatures serves as an entrypoint for manual testing of
// NVIDIA CC features.
func TestCCFeatures(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// ensure the client is started
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	_, err := NewClient(logger)
	require.NoError(err)

	caps, ret := nvml.SystemGetConfComputeCapabilities()
	assert.Equal(nvml.SUCCESS, ret)
	t.Log("CC Capabilities", caps)

	state, ret := nvml.SystemGetConfComputeState()
	assert.Equal(nvml.SUCCESS, ret)
	t.Log("CC State", state)

	device, ret := nvml.DeviceGetHandleByIndex(0)
	assert.Equal(nvml.SUCCESS, ret)

	certs, ret := device.GetConfComputeGpuCertificate()
	assert.Equal(nvml.SUCCESS, ret)
	t.Log("Certs", certs)

	var report nvml.ConfComputeGpuAttestationReport
	ret = device.GetConfComputeGpuAttestationReport(&report)
	assert.Equal(nvml.SUCCESS, ret)
	t.Log("Attestation Report", report)
}

// TestDeviceInfo tests the retrieval of GPU device information.
func TestDeviceInfo(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client, err := NewClient(logger)
	require.NoError(err)

	gpus, err := client.ListGPUs()
	require.NoError(err)
	assert.NotEmpty(gpus)

	for _, gpu := range gpus {
		info, err := gpu.Info()
		assert.NoError(err)
		assert.NotNil(info)
		t.Logf("GPU ID: %s, Architecture: %d, Driver Version: %s, VBIOS Version: %s",
			gpu.ID(), info.Architecture, info.DriverVersion, info.VBIOSVersion)
	}
}
