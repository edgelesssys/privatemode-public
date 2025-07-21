//go:build gpu
// +build gpu

package attestation

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu"
	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu/attestation/nras"
)

// Issuer issues attestation statements for local NVIDIA GPUs.
type Issuer struct {
	device *gpu.Device

	log        *slog.Logger
	nrasClient *nras.Client
}

// NewIssuer creates a new Issuer for a GPU.
func NewIssuer(device *gpu.Device, log *slog.Logger) *Issuer {
	return &Issuer{
		device:     device,
		nrasClient: nras.NewClient(log),
		log:        log,
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
func (i *Issuer) Issue(ctx context.Context, nonce [32]byte) (string, []*x509.Certificate, error) {
	i.log.Info("Issuing attestation statement for GPU", "id", i.device.ID())

	report, err := i.device.AttestationReport(nonce)
	if err != nil {
		return "", nil, fmt.Errorf("retrieving attestation report: %w", err)
	}
	i.log.Info("Retrieved attestation report")

	cert, err := i.device.Certificate()
	if err != nil {
		return "", nil, fmt.Errorf("retrieving certificate chain: %w", err)
	}
	i.log.Info("Retrieved certificate chain")

	eat, err := i.nrasClient.AttestGPU(ctx, nras.ArchHopper, nonce, report, cert)
	if err != nil {
		return "", nil, fmt.Errorf("attesting GPU through NRAS: %w", err)
	}
	i.log.Info("Retrieved EAT")

	gpuCertChain, err := parseGpuCertToX509(cert)
	if err != nil {
		return "", nil, fmt.Errorf("parsing GPU certificate chain: %w", err)
	}

	return eat, gpuCertChain, nil
}

func parseGpuCertToX509(gpuCert nvml.ConfComputeGpuCertificate) ([]*x509.Certificate, error) {
	certChain := []*x509.Certificate{}

	for certPEM, pemData := pem.Decode(gpuCert.AttestationCertChain[:]); certPEM != nil; certPEM, pemData = pem.Decode(pemData) {
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
