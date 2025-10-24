//go:build gpu

// main package of the attestation-agent.
// The attestation-agent is responsible for attesting the workload as init container in Kubernetes.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/edgelesssys/continuum/attestation-agent/internal/attestation"
	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu"
	"github.com/edgelesssys/continuum/attestation-agent/internal/ocsp"
	"github.com/edgelesssys/continuum/attestation-agent/internal/rim"
	"github.com/edgelesssys/continuum/internal/crypto"
	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/logging"
	"github.com/edgelesssys/continuum/internal/gpl/process"
	"github.com/spf13/cobra"

	internalOCSP "github.com/edgelesssys/continuum/internal/gpl/ocsp"
)

var (
	logLevel       string
	driverVersions []string
	vbiosVersions  []string
)

func main() {
	if err := execute(); err != nil {
		os.Exit(1)
	}
}

func execute() error {
	cmd := &cobra.Command{
		Use:          "attestation-agent",
		Short:        "Attestation agent for verifying the workload and obtaining secret access",
		RunE:         run,
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&logLevel, logging.Flag, logging.DefaultFlagValue, logging.FlagInfo)

	// GPU policy flags
	cmd.Flags().StringSliceVar(&driverVersions, "gpu-driver-versions", nil, "List of allowed GPU driver versions")
	must(cmd.MarkFlagRequired("gpu-driver-versions"))
	cmd.Flags().StringSliceVar(&vbiosVersions, "gpu-vbios-versions", nil, "List of allowed GPU VBIOS versions")
	must(cmd.MarkFlagRequired("gpu-vbios-versions"))

	ctx, cancel := process.SignalContext(context.Background(), os.Interrupt)
	defer cancel()
	return cmd.ExecuteContext(ctx)
}

func run(cmd *cobra.Command, _ []string) error {
	log := logging.NewLogger(logLevel)

	ocspStatus, err := verifyAndEnable(cmd.Context(), log)
	if err != nil {
		return fmt.Errorf("failed to verify GPUs: %w", err)
	}

	log.Info("Writing OCSP status to file", "file", constants.OCSPStatusFile())
	if err := os.MkdirAll(filepath.Dir(constants.OCSPStatusFile()), 0o644); err != nil {
		return fmt.Errorf("creating directory for OCSP status file: %w", err)
	}
	statusBytes, err := json.Marshal(ocspStatus)
	if err != nil {
		return fmt.Errorf("marshalling OCSP status: %w", err)
	}
	if err := os.WriteFile(constants.OCSPStatusFile(), statusBytes, 0o644); err != nil {
		return fmt.Errorf("writing OCSP status file: %w", err)
	}
	log.Info("OCSP status written successfully", "file", constants.OCSPStatusFile())

	return nil
}

// verifyAndEnable verifies the GPUs and sets them to ready state.
func verifyAndEnable(ctx context.Context, log *slog.Logger) ([]internalOCSP.StatusInfo, error) {
	// set up issuer
	gpuClient, err := gpu.NewClient(log)
	if err != nil {
		return nil, fmt.Errorf("creating GPU client: %w", err)
	}
	defer gpuClient.Close()
	availableGPUs, err := gpuClient.ListGPUs()
	if err != nil {
		return nil, fmt.Errorf("listing GPUs: %w", err)
	}
	gpuIssuers := attestation.NewIssuers(availableGPUs, log)

	rimClient := rim.New("https://rim-cache/", log) // Use the local RIM cache
	ocspClient := ocsp.New(log)

	statusInfos := make([]internalOCSP.StatusInfo, len(gpuIssuers))

	log.Info("Verifying GPUs", "amount", len(gpuIssuers))
	for i, gpuIssuer := range gpuIssuers {
		nonce, err := generateNonce()
		if err != nil {
			return nil, fmt.Errorf("generating nonce: %w", err)
		}

		report, gpuCertChain, err := gpuIssuer.Issue(nonce)
		if err != nil {
			return nil, fmt.Errorf("issuing GPU report: %w", err)
		}

		parsedReport, err := attestation.ParseReport(report)
		if err != nil {
			return nil, fmt.Errorf("parsing GPU report: %w", err)
		}

		statusInfos[i].GPU, err = ocspClient.VerifyCertChain(ctx, gpuCertChain, ocsp.VerificationModeGPUAttestation)
		if err != nil {
			return nil, fmt.Errorf("verifying GPU certificate chain: %w", err)
		}

		log.Info("Verifying GPU attestation report")
		if err := parsedReport.Verify(attestation.VerificationSettings{
			Nonce:                 nonce,
			AllowedDriverVersions: driverVersions,
			AllowedVBIOSVersions:  vbiosVersions,
			CertChain:             gpuCertChain,
		}); err != nil {
			return nil, fmt.Errorf("verifying GPU report: %w", err)
		}

		driverRIM, err := rimClient.FetchDriverRIM(ctx, rim.GPUArchHopper, parsedReport.DriverVersion())
		if err != nil {
			return nil, fmt.Errorf("fetching driver RIM: %w", err)
		}
		statusInfos[i].Driver, err = verifyRIMCertChain(ctx, driverRIM, ocsp.VerificationModeDriverRIM, ocspClient)
		if err != nil {
			return nil, fmt.Errorf("verifying driver RIM certificate chain: %w", err)
		}

		vbiosVersion, err := parsedReport.VBIOSVersion()
		if err != nil {
			return nil, fmt.Errorf("getting VBIOS version: %w", err)
		}
		vbiosRIM, err := rimClient.FetchVBIOSRIM(ctx, parsedReport.Project(), parsedReport.ProjectSKU(), parsedReport.ChipSKU(), vbiosVersion)
		if err != nil {
			return nil, fmt.Errorf("fetching VBIOS RIM: %w", err)
		}
		statusInfos[i].VBIOS, err = verifyRIMCertChain(ctx, vbiosRIM, ocsp.VerificationModeVBIOSRIM, ocspClient)
		if err != nil {
			return nil, fmt.Errorf("verifying VBIOS RIM certificate chain: %w", err)
		}

		log.Info("Validating GPU attestation report measurements")
		if err := parsedReport.ValidateMeasurements(driverRIM, vbiosRIM, nil); err != nil {
			return nil, fmt.Errorf("validating measurements: %w", err)
		}
	}
	if err := gpuClient.SetGPUsReady(); err != nil {
		return nil, fmt.Errorf("failed to set GPUs ready: %w", err)
	}

	return statusInfos, nil
}

func generateNonce() ([32]byte, error) {
	nonce, err := crypto.GenerateRandomBytes(32)
	if err != nil {
		return [32]byte{}, fmt.Errorf("generating nonce: %w", err)
	}
	return sha256.Sum256(nonce), nil
}

func verifyRIMCertChain(ctx context.Context, softwareIdentity *rim.SoftwareIdentity,
	mode ocsp.VerificationMode, ocspClient *ocsp.Client,
) (internalOCSP.Status, error) {
	certChain, err := softwareIdentity.SigningCerts()
	if err != nil {
		return internalOCSP.StatusUnknown, fmt.Errorf("parsing RIM certificates: %w", err)
	}
	ocspStatus, err := ocspClient.VerifyCertChain(ctx, certChain, mode)
	if err != nil {
		return ocspStatus, fmt.Errorf("verifying RIM certificate chain: %w", err)
	}
	return ocspStatus, nil
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
