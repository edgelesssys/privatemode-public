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

	"golang.org/x/crypto/ocsp"
)

//go:embed gpu_device_identity_ca.pem
var gpuDeviceIdentityCACertPEM []byte

var gpuDeviceIdentityCACert = mustParseCertificate(gpuDeviceIdentityCACertPEM)

// VerificationMode defines what type of certificate chain is being verified.
type VerificationMode int

const (
	// VerificationModeGPUAttestation is used for verifying GPU attestation certificates.
	VerificationModeGPUAttestation VerificationMode = iota
	// VerificationModeVBIOSRIM is used for verifying certificates returned by the RIM service for VBIOS reference values.
	VerificationModeVBIOSRIM
	// VerificationModeDriverRIM is used for verifying certificates returned by the RIM service for Driver reference values.
	VerificationModeDriverRIM
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
func (c *Client) VerifyCertChain(ctx context.Context, certChain []*x509.Certificate, mode VerificationMode) error {
	// Set root CA to a known good trust anchor
	switch mode {
	case VerificationModeGPUAttestation:
		certChain[(len(certChain) - 1)] = gpuDeviceIdentityCACert
	}

	// Start by verifying the certificate chain
	if len(certChain) < 2 {
		return errors.New("certificate chain must contain at least two certificates")
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
		return fmt.Errorf("verifying certificate chain: %w", err)
	}

	// The leaf certificate of the GPU attestation cert chain is not registered in the OCSP responder,
	// since its generated "on-demand" by the GPU. Instead, the second certificate in the chain,
	// the device identity certificate, is enough to check for revocation.
	if mode == VerificationModeGPUAttestation {
		certChain = certChain[1:]
	}

	// Check if any of the certificates where revoked by NVIDIA's OCSP server
	c.log.Info("Checking OCSP status of certificate chain", "mode", mode)
	for i := range len(certChain) - 1 {
		if err := c.verifyCertificate(ctx, certChain[i], certChain[i+1]); err != nil {
			return fmt.Errorf("OCSP verification failed for certificate %d: %w", i, err)
		}
	}

	return nil
}

func (c *Client) verifyCertificate(ctx context.Context, cert, issuer *x509.Certificate) error {
	reqBytes, err := ocsp.CreateRequest(cert, issuer, &ocsp.RequestOptions{
		Hash: crypto.SHA384, // NVIDIA uses SHA-384 for OCSP requests
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(reqBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/ocsp-request")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %s: %s", resp.Status, string(respBody))
	}

	ocspResp, err := ocsp.ParseResponse(respBody, issuer)
	if err != nil {
		return fmt.Errorf("failed to parse OCSP response: %w", err)
	}

	if ocspResp.Status != ocsp.Good {
		var msg string
		switch ocspResp.Status {
		case ocsp.Revoked:
			msg = "certificate is revoked"
		case ocsp.Unknown:
			msg = "certificate is unknown to the OCSP responder"
		default:
			msg = fmt.Sprintf("unexpected OCSP status %d", ocspResp.Status)
		}
		return fmt.Errorf("OCSP status verification failed: %s", msg)
	}

	return nil
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
