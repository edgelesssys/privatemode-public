//go:build gpu

// main package of the attestation-agent.
// The attestation-agent is responsible for attesting the workload as init container in Kubernetes.
package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"

	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu"
	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu/attestation"
	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu/attestation/policy"
	"github.com/edgelesssys/continuum/attestation-agent/internal/secretapi"
	"github.com/edgelesssys/continuum/internal/crypto"
	"github.com/edgelesssys/continuum/internal/gpl/contrast"
	"github.com/edgelesssys/continuum/internal/gpl/logging"
	"github.com/edgelesssys/continuum/internal/gpl/process"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var (
	ssAddress string
	logLevel  string

	// GPU policy flags.
	debugMode      bool
	secureBoot     bool
	eatVersion     string
	driverVersions []string
	vbiosVersions  []string
)

func main() {
	cmd := &cobra.Command{
		Use:          "attestation-agent",
		Short:        "Attestation agent for verifying the workload and obtaining secret access",
		RunE:         run,
		SilenceUsage: true,
	}

	// Register flags
	cmd.Flags().StringVar(&ssAddress, "secret-svc-address", "", "host name or IP for the secret service")
	must(cmd.MarkFlagRequired("secret-svc-address"))
	cmd.Flags().StringVar(&logLevel, logging.Flag, logging.DefaultFlagValue, logging.FlagInfo)

	// GPU policy flags
	cmd.Flags().BoolVar(&debugMode, "gpu-debug", false, "Enable GPU debug mode")
	must(cmd.MarkFlagRequired("gpu-debug"))
	cmd.Flags().BoolVar(&secureBoot, "gpu-secure-boot", true, "Require GPU secure boot")
	must(cmd.MarkFlagRequired("gpu-secure-boot"))
	cmd.Flags().StringVar(&eatVersion, "gpu-eat-version", "", "GPU EAT version")
	must(cmd.MarkFlagRequired("gpu-eat-version"))
	cmd.Flags().StringSliceVar(&driverVersions, "gpu-driver-versions", nil, "List of allowed GPU driver versions")
	must(cmd.MarkFlagRequired("gpu-driver-versions"))
	cmd.Flags().StringSliceVar(&vbiosVersions, "gpu-vbios-versions", nil, "List of allowed GPU VBIOS versions")
	must(cmd.MarkFlagRequired("gpu-vbios-versions"))

	ctx, cancel := process.SignalContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := cmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, _ []string) error {
	log := logging.NewLogger(logLevel)
	fs := afero.Afero{Fs: afero.NewOsFs()}

	gpuPolicy := parseGPUPolicyFromFlags()
	if err := verifyAndEnable(cmd.Context(), gpuPolicy, log); err != nil {
		return fmt.Errorf("failed to verify GPUs: %w", err)
	}

	tlsConfig, err := contrast.ClientTLSConfigFromDir("")
	if err != nil {
		return fmt.Errorf("failed to create TLS config: %w", err)
	}
	secretAccess, err := secretapi.RequestSecretAccess(cmd.Context(), log, ssAddress, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to request secret access: %w", err)
	}
	if err := secretapi.SaveSecretAccessCerts(fs, secretAccess); err != nil {
		return fmt.Errorf("failed to save secret access certificates: %w", err)
	}

	return nil
}

// verifyAndEnable verifies the GPUs and sets them to ready state.
func verifyAndEnable(ctx context.Context, gpuPolicy *policy.NvidiaHopper, log *slog.Logger) error {
	// set up issuer
	gpuClient, err := gpu.NewClient(log)
	if err != nil {
		return fmt.Errorf("creating GPU client: %w", err)
	}
	defer gpuClient.Close()
	availableGPUs, err := gpuClient.ListGPUs()
	if err != nil {
		return fmt.Errorf("listing GPUs: %w", err)
	}
	gpuIssuers := attestation.NewIssuers(availableGPUs, log)

	gpuVerifier := attestation.NewVerifier(gpuPolicy, log)
	log.Info("Verifying GPUs", "amount", len(gpuIssuers))
	for _, gpuIssuer := range gpuIssuers {
		nonce, err := generateNonce()
		if err != nil {
			return fmt.Errorf("generating nonce: %w", err)
		}
		gpuEAT, err := gpuIssuer.Issue(ctx, nonce)
		if err != nil {
			return fmt.Errorf("issuing GPU report: %w", err)
		}
		if err := gpuVerifier.Verify(ctx, gpuEAT, nonce); err != nil {
			return fmt.Errorf("verifying GPU report: %w", err)
		}
	}
	if err := gpuClient.SetGPUsReady(); err != nil {
		return fmt.Errorf("failed to set GPUs ready: %w", err)
	}
	return nil
}

func generateNonce() ([32]byte, error) {
	nonce, err := crypto.GenerateRandomBytes(32)
	if err != nil {
		return [32]byte{}, fmt.Errorf("generating nonce: %w", err)
	}
	return sha256.Sum256(nonce), nil
}

// parseGPUPolicyFromFlags parses the GPU policy from command line flags.
func parseGPUPolicyFromFlags() *policy.NvidiaHopper {
	return &policy.NvidiaHopper{
		Debug:                   debugMode,
		SecureBoot:              secureBoot,
		EATVersion:              eatVersion,
		DriverVersions:          driverVersions,
		VBIOSVersions:           vbiosVersions,
		MismatchingMeasurements: nil,
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
