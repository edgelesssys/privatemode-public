package attestation

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu/attestation/policy"
	"github.com/golang-jwt/jwt/v5"
)

// eatClaims are the claims contained within an EAT.
type eatClaims struct {
	Sub                              string `json:"sub"`
	Secboot                          bool   `json:"secboot"`
	XNvidiaGpuManufacturer           string `json:"x-nvidia-gpu-manufacturer"`
	XNvidiaAttestationType           string `json:"x-nvidia-attestation-type"`
	Iss                              string `json:"iss"`
	EatNonce                         string `json:"eat_nonce"`
	XNvidiaAttestationDetailedResult struct {
		XNvidiaGpuDriverRimSchemaValidated bool `json:"x-nvidia-gpu-driver-rim-schema-validated"`
		XNvidiaGpuVbiosRimCertValidated    bool `json:"x-nvidia-gpu-vbios-rim-cert-validated"`
		XNvidiaMismatchMeasurementRecords  []struct {
			RuntimeSize  int    `json:"runtimeSize"`
			Index        int    `json:"index"`
			GoldenValue  string `json:"goldenValue"`
			RuntimeValue string `json:"runtimeValue"`
			GoldenSize   int    `json:"goldenSize"`
		} `json:"x-nvidia-mismatch-measurement-records"`
		XNvidiaGpuAttestationReportCertChainValidated  bool  `json:"x-nvidia-gpu-attestation-report-cert-chain-validated"`
		XNvidiaGpuDriverRimSchemaFetched               bool  `json:"x-nvidia-gpu-driver-rim-schema-fetched"`
		XNvidiaGpuAttestationReportParsed              bool  `json:"x-nvidia-gpu-attestation-report-parsed"`
		XNvidiaGpuNonceMatch                           bool  `json:"x-nvidia-gpu-nonce-match"`
		XNvidiaGpuVbiosRimSignatureVerified            bool  `json:"x-nvidia-gpu-vbios-rim-signature-verified"`
		XNvidiaGpuDriverRimSignatureVerified           bool  `json:"x-nvidia-gpu-driver-rim-signature-verified"`
		XNvidiaGpuArchCheck                            bool  `json:"x-nvidia-gpu-arch-check"`
		XNvidiaAttestationWarning                      any   `json:"x-nvidia-attestation-warning"`
		XNvidiaGpuMeasurementsMatch                    bool  `json:"x-nvidia-gpu-measurements-match"`
		XNvidiaMismatchIndexes                         []int `json:"x-nvidia-mismatch-indexes"`
		XNvidiaGpuAttestationReportSignatureVerified   bool  `json:"x-nvidia-gpu-attestation-report-signature-verified"`
		XNvidiaGpuVbiosRimSchemaValidated              bool  `json:"x-nvidia-gpu-vbios-rim-schema-validated"`
		XNvidiaGpuDriverRimCertValidated               bool  `json:"x-nvidia-gpu-driver-rim-cert-validated"`
		XNvidiaGpuVbiosRimSchemaFetched                bool  `json:"x-nvidia-gpu-vbios-rim-schema-fetched"`
		XNvidiaGpuVbiosRimMeasurementsAvailable        bool  `json:"x-nvidia-gpu-vbios-rim-measurements-available"`
		XNvidiaGpuDriverRimDriverMeasurementsAvailable bool  `json:"x-nvidia-gpu-driver-rim-driver-measurements-available"`
	} `json:"x-nvidia-attestation-detailed-result"`
	XNvidiaVer              string `json:"x-nvidia-ver"`
	Nbf                     int    `json:"nbf"`
	XNvidiaGpuDriverVersion string `json:"x-nvidia-gpu-driver-version"`
	Dbgstat                 string `json:"dbgstat"`
	Hwmodel                 string `json:"hwmodel"`
	Oemid                   string `json:"oemid"`
	Measres                 string `json:"measres"`
	Exp                     int    `json:"exp"`
	Iat                     int    `json:"iat"`
	XNvidiaEatVer           string `json:"x-nvidia-eat-ver"`
	Ueid                    string `json:"ueid"`
	XNvidiaGpuVbiosVersion  string `json:"x-nvidia-gpu-vbios-version"`
	Jti                     string `json:"jti"`
}

func appraiseEAT(policy *policy.NvidiaHopper, token *jwt.Token, nonce [32]byte) error {
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return errors.New("invalid EAT data")
	}

	rawClaims, err := json.Marshal(claims)
	if err != nil {
		return fmt.Errorf("marshalling claims: %w", err)
	}

	var eatClaims eatClaims
	if err := json.Unmarshal(rawClaims, &eatClaims); err != nil {
		return fmt.Errorf("unmarshalling claims: %w", err)
	}

	if err := validateClaims(policy, eatClaims, nonce); err != nil {
		return fmt.Errorf("validating claims: %w", err)
	}

	return nil
}

// validateClaims contains the validation logic for checking the claims within
// an EAT. This explicitly does not check the default JWT claims (exp, nbf, iat, ...)
// as those are expected to be checked when verifying the signature, which is not
// the responsibility of the appraisal policy.
func validateClaims(policy *policy.NvidiaHopper, claims eatClaims, nonce [32]byte) (retErr error) {
	// From what we saw until now, EAT versions aren't backwards-compatible, so we
	// should fail if we see an unexpected version.
	if claims.XNvidiaEatVer != policy.EATVersion {
		retErr = errors.Join(retErr, fmt.Errorf("unexpected EAT version: expected: %s, got: %s", policy.EATVersion, claims.XNvidiaEatVer))
	}

	expectedNonce := hex.EncodeToString(nonce[:])
	if !strings.EqualFold(claims.EatNonce, expectedNonce) {
		retErr = errors.Join(retErr, fmt.Errorf("nonce mismatch: expected: %s, got: %s", expectedNonce, claims.EatNonce))
	}

	if !slices.Contains(policy.DriverVersions, claims.XNvidiaGpuDriverVersion) {
		retErr = errors.Join(retErr, fmt.Errorf("disallowed driver version: expected one of: %v, got: %s", policy.DriverVersions, claims.XNvidiaGpuDriverVersion))
	}

	if !slices.Contains(policy.VBIOSVersions, claims.XNvidiaGpuVbiosVersion) {
		retErr = errors.Join(retErr, fmt.Errorf("disallowed VBIOS version: expected one of: %v, got: %s", policy.VBIOSVersions, claims.XNvidiaGpuVbiosVersion))
	}

	if claims.Dbgstat != "disabled" && !policy.Debug {
		retErr = errors.Join(retErr, fmt.Errorf("disallowed debug status: %s: GPU is not allowed to run in debug mode", claims.Dbgstat))
	}

	if claims.Secboot != policy.SecureBoot {
		retErr = errors.Join(retErr, fmt.Errorf("disallowed SecureBoot mode: expected: %t, got: %t", policy.SecureBoot, claims.Secboot))
	}

	for _, idx := range claims.XNvidiaAttestationDetailedResult.XNvidiaMismatchIndexes {
		if !slices.Contains(policy.MismatchingMeasurements, idx) {
			retErr = errors.Join(retErr, fmt.Errorf("unwanted measurements mismatch: index %d does not match", idx))
		}
	}

	if err := staticPolicy(claims); err != nil {
		retErr = errors.Join(retErr, err)
	}

	return retErr
}

// staticPolicy enforces static policy checks that are not meant to be configured by users.
func staticPolicy(claims eatClaims) (retErr error) {
	if claims.Measres != "comparison-successful" {
		retErr = errors.Join(retErr, errors.New("measurement comparison failed"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuDriverRimDriverMeasurementsAvailable {
		retErr = errors.Join(retErr, errors.New("driver RIM driver measurements not available"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuDriverRimSchemaFetched {
		retErr = errors.Join(retErr, errors.New("driver RIM schema not fetched"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuDriverRimSchemaValidated {
		retErr = errors.Join(retErr, errors.New("driver RIM schema not validated"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuDriverRimSignatureVerified {
		retErr = errors.Join(retErr, errors.New("driver RIM signature not verified"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuVbiosRimMeasurementsAvailable {
		retErr = errors.Join(retErr, errors.New("VBIOS RIM measurements not available"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuVbiosRimSchemaFetched {
		retErr = errors.Join(retErr, errors.New("VBIOS RIM schema not fetched"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuVbiosRimSchemaValidated {
		retErr = errors.Join(retErr, errors.New("VBIOS RIM schema not validated"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuVbiosRimCertValidated {
		retErr = errors.Join(retErr, errors.New("VBIOS RIM cert not validated"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuVbiosRimSignatureVerified {
		retErr = errors.Join(retErr, errors.New("VBIOS RIM signature not verified"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuAttestationReportCertChainValidated {
		retErr = errors.Join(retErr, errors.New("attestation report cert chain not validated"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuAttestationReportParsed {
		retErr = errors.Join(retErr, errors.New("attestation report not parsed"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuAttestationReportSignatureVerified {
		retErr = errors.Join(retErr, errors.New("attestation report signature not verified"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuNonceMatch {
		retErr = errors.Join(retErr, errors.New("nonce mismatch"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuArchCheck {
		retErr = errors.Join(retErr, errors.New("GPU architecture check failed"))
	}

	if !claims.XNvidiaAttestationDetailedResult.XNvidiaGpuMeasurementsMatch {
		retErr = errors.Join(retErr, errors.New("measurements mismatch"))
	}

	return retErr
}
