//go:build gpu

package gpu

import (
	"fmt"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

/*
Normally, the code in here would go into `device.go`, but as the NVML needs special
treatment when being linked, we need to put it behind build tags.
*/

// AttestationReport returns the attestation report for the given GPU.
func (d *Device) AttestationReport(nonce [32]byte) (nvml.ConfComputeGpuAttestationReport, error) {
	device, err := d.handle()
	if err != nil {
		return nvml.ConfComputeGpuAttestationReport{}, fmt.Errorf("getting GPU handle: %w", err)
	}

	report, ret := device.GetConfComputeGpuAttestationReport(nonce)
	if ret != nvml.SUCCESS {
		return nvml.ConfComputeGpuAttestationReport{}, fmt.Errorf("getting GPU attestation report: %s", nvml.ErrorString(ret))
	}

	return report, nil
}

// Certificate returns the attestation certificate for the given GPU.
func (d *Device) Certificate() (nvml.ConfComputeGpuCertificate, error) {
	device, err := d.handle()
	if err != nil {
		return nvml.ConfComputeGpuCertificate{}, fmt.Errorf("getting GPU handle: %w", err)
	}

	cert, ret := device.GetConfComputeGpuCertificate()
	if ret != nvml.SUCCESS {
		return nvml.ConfComputeGpuCertificate{}, fmt.Errorf("getting GPU attestation certificate: %s", nvml.ErrorString(ret))
	}

	return cert, nil
}
