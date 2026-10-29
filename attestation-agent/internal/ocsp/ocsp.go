// Package ocsp interacts with NVIDIA's OCSP server to check the status of GPU and RIM certificates.
package ocsp

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	_ "embed"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"slices"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/httputil"
	internalOCSP "github.com/edgelesssys/continuum/internal/oss/ocsp"
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
		client: httputil.NewClient(),
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

	// Require the presented root to match the pinned trust anchor without
	// modifying the caller's certificate chain.
	var root *x509.Certificate
	switch mode {
	case VerificationModeGPUAttestation:
		root = gpuDeviceIdentityCACert
	case VerificationModeVBIOSRIM, VerificationModeDriverRIM:
		root = rimSigningRootCACert
	default:
		return internalOCSP.StatusUnknown, fmt.Errorf("unknown verification mode: %q", mode)
	}
	if !certChain[len(certChain)-1].Equal(root) {
		return internalOCSP.StatusUnknown, errors.New("presented root does not match pinned trust anchor")
	}

	rootPool := x509.NewCertPool()
	rootPool.AddCert(root)
	intermediatePool := x509.NewCertPool()
	for _, cert := range certChain[1 : len(certChain)-1] {
		intermediatePool.AddCert(cert)
	}

	c.log.InfoContext(ctx, "Verifying certificate chain", "mode", mode)
	verifiedChains, err := certChain[0].Verify(x509.VerifyOptions{
		Roots:         rootPool,
		Intermediates: intermediatePool,
	})
	if err != nil {
		return internalOCSP.StatusUnknown, fmt.Errorf("verifying certificate chain: %w", err)
	}
	// Verify may ignore presented certificates. Require the full chain in the
	// same order so every certificate and issuer used below has been verified.
	if !slices.ContainsFunc(verifiedChains, func(chain []*x509.Certificate) bool {
		return slices.EqualFunc(chain, certChain, (*x509.Certificate).Equal)
	}) {
		return internalOCSP.StatusUnknown, errors.New("verified certificate chain does not match presented certificate chain")
	}

	// The leaf certificate of the GPU attestation cert chain is not registered in the OCSP responder,
	// since its generated "on-demand" by the GPU. Instead, the second certificate in the chain,
	// the device identity certificate, is enough to check for revocation.
	if mode == VerificationModeGPUAttestation {
		certChain = certChain[1:]
	}

	// Check if any of the certificates where revoked by NVIDIA's OCSP server
	c.log.InfoContext(ctx, "Checking OCSP status of certificate chain", "mode", mode)
	statusResponses := []internalOCSP.Status{}
	for i := range len(certChain) - 1 {
		status, err := c.verifyCertificate(ctx, certChain[i], certChain[i+1])
		if err != nil {
			c.log.ErrorContext(ctx, "OCSP verification failed", "error", err,
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

	ocspResp, err := ocsp.ParseResponseForCert(respBody, cert, issuer)
	if err != nil {
		return internalOCSP.StatusUnknown, fmt.Errorf("failed to parse OCSP response: %w", err)
	}
	if err := verifyCertID(ocspResp, cert, issuer); err != nil {
		return internalOCSP.StatusUnknown, fmt.Errorf("verifying OCSP certificate identity: %w", err)
	}

	now := time.Now()
	// Allow bounded responder clock skew, but never extend the signed expiry.
	const clockSkew = 5 * time.Minute
	if ocspResp.ThisUpdate.IsZero() || ocspResp.ThisUpdate.After(now.Add(clockSkew)) ||
		!ocspResp.NextUpdate.After(now) || !ocspResp.NextUpdate.After(ocspResp.ThisUpdate) {
		return internalOCSP.StatusUnknown, fmt.Errorf("OCSP response is outside its validity interval: now=%s thisUpdate=%s nextUpdate=%s allowedClockSkew=%s",
			now.UTC().Format(time.RFC3339Nano), ocspResp.ThisUpdate.UTC().Format(time.RFC3339Nano),
			ocspResp.NextUpdate.UTC().Format(time.RFC3339Nano), clockSkew)
	}
	// ParseResponseForCert checks signatures, but not the delegated responder's
	// authorization or certificate validity. The issuing CA itself needs no EKU.
	if responder := ocspResp.Certificate; responder != nil && !responder.Equal(issuer) {
		if !slices.Contains(responder.ExtKeyUsage, x509.ExtKeyUsageOCSPSigning) {
			return internalOCSP.StatusUnknown, errors.New("OCSP responder lacks OCSP signing authorization")
		}
		if responder.KeyUsage != 0 && responder.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
			return internalOCSP.StatusUnknown, errors.New("OCSP responder key does not permit signing")
		}
		roots := x509.NewCertPool()
		roots.AddCert(issuer)
		if _, err := responder.Verify(x509.VerifyOptions{
			Roots: roots, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageOCSPSigning},
		}); err != nil {
			return internalOCSP.StatusUnknown, fmt.Errorf("verifying OCSP responder certificate: %w", err)
		}
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

// verifyCertID completes ParseResponseForCert's serial-number check with the
// issuer hashes, which x/crypto/ocsp does not expose or verify. Only call this
// after ParseResponseForCert has validated the response and its signature.
func verifyCertID(response *ocsp.Response, cert, issuer *x509.Certificate) error {
	// Decode just the prefixes needed from the already-validated ResponseData
	// and SingleResponse sequences (RFC 6960 section 4.2.1).
	var data struct {
		Version     int `asn1:"optional,default:0,explicit,tag:0"`
		ResponderID asn1.RawValue
		ProducedAt  time.Time `asn1:"generalized"`
		Responses   []struct {
			CertID struct {
				HashAlgorithm asn1.RawValue
				NameHash      []byte
				KeyHash       []byte
				SerialNumber  *big.Int
			}
		}
	}
	if _, err := asn1.Unmarshal(response.TBSResponseData, &data); err != nil {
		return err
	}
	// Reuse the OCSP request encoder to calculate the expected issuer hashes.
	// The responder may use a different hash algorithm from our request.
	encoded, err := ocsp.CreateRequest(cert, issuer, &ocsp.RequestOptions{Hash: response.IssuerHash})
	if err != nil {
		return err
	}
	expected, err := ocsp.ParseRequest(encoded)
	if err != nil {
		return err
	}
	for _, single := range data.Responses {
		id := single.CertID
		if id.SerialNumber.Cmp(cert.SerialNumber) != 0 {
			continue
		}
		// Match the FIRST equal serial, exactly as ParseResponseForCert does;
		// a later matching CertID must not authenticate an earlier status.
		if !bytes.Equal(id.NameHash, expected.IssuerNameHash) || !bytes.Equal(id.KeyHash, expected.IssuerKeyHash) {
			return errors.New("OCSP response issuer does not match certificate issuer")
		}
		return nil
	}
	return errors.New("OCSP response does not identify certificate")
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
