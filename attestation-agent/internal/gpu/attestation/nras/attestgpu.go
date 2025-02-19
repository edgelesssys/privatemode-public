//go:build gpu

package nras

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// AttestGPU issues an Entity Attestation Token (EAT) for the given GPU.
func (c *Client) AttestGPU(ctx context.Context, arch Arch, nonce [32]byte,
	report nvml.ConfComputeGpuAttestationReport, certChain nvml.ConfComputeGpuCertificate,
) (string, error) {
	c.log.Info("Attesting GPU")

	if len(report.AttestationReport) < int(report.AttestationReportSize) {
		return "", fmt.Errorf("invalid attestation report size, got %d, want at least %d",
			len(report.AttestationReport), report.AttestationReportSize)
	}

	if len(certChain.AttestationCertChain) < int(certChain.AttestationCertChainSize) {
		return "", fmt.Errorf("invalid attestation certificate chain size, got %d, want at least %d",
			len(certChain.AttestationCertChain), certChain.AttestationCertChainSize)
	}

	encodedEvidence := base64.StdEncoding.EncodeToString(
		[]byte(hex.EncodeToString(report.AttestationReport[:report.AttestationReportSize])),
	)

	encodedCertChain := base64.StdEncoding.EncodeToString(
		certChain.AttestationCertChain[:certChain.AttestationCertChainSize],
	)

	encodedNonce := hex.EncodeToString(nonce[:])

	body, err := json.Marshal(attestGPURequestBody{
		Arch:        arch,
		Nonce:       encodedNonce,
		Evidence:    encodedEvidence,
		Certificate: encodedCertChain,
	})
	if err != nil {
		return "", fmt.Errorf("marshalling request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://nras.attestation.nvidia.com/v1/attest/gpu", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("performing request: %w", err)
	}
	defer res.Body.Close()

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("reading response body: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d, body: %s", res.StatusCode, string(resBody))
	}

	var resBodyObj attestGPUResponseBody
	if err := json.Unmarshal(resBody, &resBodyObj); err != nil {
		return "", fmt.Errorf("unmarshalling response body: %w", err)
	}

	return resBodyObj.EAT, nil
}

// attestGPURequestBody is the request body format used by the NRAS.
// See https://docs.attestation.nvidia.com/api-docs/nras.html#post-/v1/attest/gpu.
type attestGPURequestBody struct {
	Nonce       string `json:"nonce"`
	Arch        Arch   `json:"arch"`
	Evidence    string `json:"evidence"`
	Certificate string `json:"certificate"`
}

// attestGPUResponseBody is the response body format used by the NRAS.
type attestGPUResponseBody struct {
	EAT string `json:"eat"`
}
