// Package ocsp interacts with NVIDIA's OCSP server to check the status of GPU and RIM certificates.
package ocsp

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	_ "embed"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	internalOCSP "github.com/edgelesssys/continuum/internal/gpl/ocsp"
	"golang.org/x/crypto/ocsp"
)

//go:embed gpu_device_identity_ca.pem
var gpuDeviceIdentityCACertPEM []byte

//go:embed rim_signing_root_ca.pem
var rimSigningRootCACertPEM []byte

var (
	gpuDeviceIdentityCACert = mustParseCertificate(gpuDeviceIdentityCACertPEM)
	rimSigningRootCACert    = mustParseCertificate(rimSigningRootCACertPEM)
)

// VerificationMode defines what type of certificate chain is being verified.
type VerificationMode string

const (
	// VerificationModeGPUAttestation is used for verifying GPU attestation certificates.
	VerificationModeGPUAttestation VerificationMode = "GPU_ATTESTATION"
	// VerificationModeVBIOSRIM is used for verifying certificates returned by the RIM service for VBIOS reference values.
	VerificationModeVBIOSRIM VerificationMode = "VBIOS_RIM_CERT"
	// VerificationModeDriverRIM is used for verifying certificates returned by the RIM service for Driver reference values.
	VerificationModeDriverRIM VerificationMode = "DRIVER_RIM_CERT"
)

const nvidiaOCSPURL = "https://ocsp.ndis.nvidia.com"

// Client interacts with NVIDIA's OCSP server to validate certificate chains.
type Client struct {
	client *http.Client
	url    string
	log    *slog.Logger
}

// New sets up a new client for NVIDIA's OCSP server.
func New(log *slog.Logger) *Client {
	return &Client{
		client: &http.Client{},
		url:    nvidiaOCSPURL,
		log:    log,
	}
}

// VerifyCertChain checks the status of a certificate against NVIDIA's OCSP server.
func (c *Client) VerifyCertChain(ctx context.Context,
	certChain []*x509.Certificate, mode VerificationMode,
) (internalOCSP.Status, error) {
	// Start by verifying the certificate chain
	if len(certChain) < 2 {
		return internalOCSP.StatusUnknown, errors.New("certificate chain must contain at least two certificates")
	}

	// Set root CA to a known good trust anchor
	switch mode {
	case VerificationModeGPUAttestation:
		certChain[(len(certChain) - 1)] = gpuDeviceIdentityCACert
	case VerificationModeVBIOSRIM, VerificationModeDriverRIM:
		certChain[(len(certChain) - 1)] = rimSigningRootCACert
	}

	rootPool := x509.NewCertPool()
	rootPool.AddCert(certChain[len(certChain)-1])
	intermediatePool := x509.NewCertPool()
	for _, cert := range certChain[1 : len(certChain)-1] {
		intermediatePool.AddCert(cert)
	}

	c.log.Info("Verifying certificate chain", "mode", mode)
	if _, err := certChain[0].Verify(x509.VerifyOptions{
		Roots:         rootPool,
		Intermediates: intermediatePool,
	}); err != nil {
		return internalOCSP.StatusUnknown, fmt.Errorf("verifying certificate chain: %w", err)
	}

	// The leaf certificate of the GPU attestation cert chain is not registered in the OCSP responder,
	// since its generated "on-demand" by the GPU. Instead, the second certificate in the chain,
	// the device identity certificate, is enough to check for revocation.
	if mode == VerificationModeGPUAttestation {
		certChain = certChain[1:]
	}

	// Check if any of the certificates where revoked by NVIDIA's OCSP server
	c.log.Info("Checking OCSP status of certificate chain", "mode", mode)
	statusResponses := []internalOCSP.Status{}
	for i := range len(certChain) - 1 {
		status, err := c.verifyCertificate(ctx, certChain[i], certChain[i+1])
		if err != nil {
			c.log.Error("OCSP verification failed", "error", err,
				"cert", certChain[i].Subject.CommonName, "issuer", certChain[i+1].Subject.CommonName)
		}
		statusResponses = append(statusResponses, status)
	}

	return internalOCSP.CombineStatuses(statusResponses), nil
}

func (c *Client) verifyCertificate(ctx context.Context, cert, issuer *x509.Certificate) (internalOCSP.Status, error) {
	reqBytes, err := ocsp.CreateRequest(cert, issuer, &ocsp.RequestOptions{
		Hash: crypto.SHA384, // NVIDIA uses SHA-384 for OCSP requests
	})
	if err != nil {
		return internalOCSP.StatusUnknown, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(reqBytes))
	if err != nil {
		return internalOCSP.StatusUnknown, err
	}
	req.Header.Set("Content-Type", "application/ocsp-request")

	resp, err := c.client.Do(req)
	if err != nil {
		return internalOCSP.StatusUnknown, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return internalOCSP.StatusUnknown, err
	}
	if resp.StatusCode != http.StatusOK {
		return internalOCSP.StatusUnknown, fmt.Errorf("unexpected status code %s: %s", resp.Status, string(respBody))
	}

	ocspResp, err := ocsp.ParseResponse(respBody, issuer)
	if err != nil {
		return internalOCSP.StatusUnknown, fmt.Errorf("failed to parse OCSP response: %w", err)
	}

	status := internalOCSP.StatusGood
	if ocspResp.Status != ocsp.Good {
		var msg string
		switch ocspResp.Status {
		case ocsp.Revoked:
			msg = "certificate is revoked"
			status = internalOCSP.StatusRevoked(ocspResp.RevokedAt)
		case ocsp.Unknown:
			msg = "certificate is unknown to the OCSP responder"
			status = internalOCSP.StatusUnknown
		default:
			msg = fmt.Sprintf("unexpected OCSP status %d", ocspResp.Status)
			status = internalOCSP.StatusUnknown
		}
		return status, fmt.Errorf("OCSP status verification failed: %s", msg)
	}

	return status, nil
}

func mustParseCertificate(pemData []byte) *x509.Certificate {
	pemBlock, _ := pem.Decode(pemData)
	if pemBlock == nil {
		panic("failed to decode PEM data")
	}
	cert, err := x509.ParseCertificate(pemBlock.Bytes)
	if err != nil {
		panic(fmt.Sprintf("failed to parse certificate: %s", err))
	}
	return cert
}
