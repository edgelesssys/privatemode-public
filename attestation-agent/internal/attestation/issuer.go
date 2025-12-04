//go:build gpu

package attestation

import (
	"crypto/x509"
	"fmt"
	"log/slog"

	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu"
)

// Issuer issues attestation statements for local NVIDIA GPUs.
type Issuer struct {
	device *gpu.Device

	log *slog.Logger
}

// NewIssuer creates a new Issuer for a GPU.
func NewIssuer(device *gpu.Device, log *slog.Logger) *Issuer {
	return &Issuer{
		device: device,
		log:    log,
	}
}

// NewIssuers creates a new Issuer for all available GPUs.
func NewIssuers(devices []*gpu.Device, log *slog.Logger) []*Issuer {
	issuers := make([]*Issuer, len(devices))
	for i, device := range devices {
		issuers[i] = NewIssuer(device, log)
	}
	return issuers
}

/*
Issue issues an attestation statement for the given GPU.

It does so by requesting an attestation report and certificate
chain for the given GPU device and nonce, and then sending the nonce, report, and certificate
chain to the NRAS. The NRAS then issues an EAT (Entity Attestation Token) for the
given GPU, if the supplied data is valid.

It returns the base64-encoded EAT (Entity Attestation Token), as per
https://docs.attestation.nvidia.com/api-docs/nras.html#operation/attestGPU.
*/
func (i *Issuer) Issue(nonce [32]byte) ([]byte, []*x509.Certificate, error) {
	i.log.Info("Issuing attestation statement for GPU", "id", i.device.ID())

	report, err := i.device.AttestationReport(nonce)
	if err != nil {
		return nil, nil, fmt.Errorf("retrieving attestation report: %w", err)
	}
	i.log.Info("Retrieved attestation report")

	gpuCertChain, err := i.device.Certificate()
	if err != nil {
		return nil, nil, fmt.Errorf("retrieving certificate chain: %w", err)
	}
	i.log.Info("Retrieved certificate chain")

	return report, gpuCertChain, nil
}
