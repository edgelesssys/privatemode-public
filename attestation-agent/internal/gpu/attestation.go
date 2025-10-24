//go:build gpu

package gpu

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

/*
Normally, the code in here would go into `device.go`, but as the NVML needs special
treatment when being linked, we need to put it behind build tags.
*/

// AttestationReport returns the attestation report for the given GPU.
func (d *Device) AttestationReport(nonce [32]byte) ([]byte, error) {
	device, err := d.handle()
	if err != nil {
		return nil, fmt.Errorf("getting GPU handle: %w", err)
	}

	report := nvml.ConfComputeGpuAttestationReport{Nonce: nonce}
	if ret := device.GetConfComputeGpuAttestationReport(&report); ret != nvml.SUCCESS {
		return nil, fmt.Errorf("getting GPU attestation report: %s", nvml.ErrorString(ret))
	}

	if len(report.AttestationReport) < int(report.AttestationReportSize) {
		return nil, fmt.Errorf("invalid attestation report size: expected %d bytes, got %d", report.AttestationReportSize, len(report.AttestationReport))
	}

	return report.AttestationReport[:report.AttestationReportSize], nil
}

// Certificate returns the attestation certificate chain for the given GPU.
func (d *Device) Certificate() ([]*x509.Certificate, error) {
	device, err := d.handle()
	if err != nil {
		return nil, fmt.Errorf("getting GPU handle: %w", err)
	}

	cert, ret := device.GetConfComputeGpuCertificate()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("getting GPU attestation certificate: %s", nvml.ErrorString(ret))
	}

	certChain := []*x509.Certificate{}
	for certPEM, pemData := pem.Decode(cert.AttestationCertChain[:]); certPEM != nil; certPEM, pemData = pem.Decode(pemData) {
		x509Cert, err := x509.ParseCertificate(certPEM.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parsing certificate: %w", err)
		}
		certChain = append(certChain, x509Cert)
	}

	if len(certChain) == 0 {
		return nil, fmt.Errorf("no certificates found in GPU certificate chain")
	}

	return certChain, nil
}
