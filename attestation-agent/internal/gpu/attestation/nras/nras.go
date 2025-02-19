// The NRAS package implements a client to talk to the NVIDIA Remote Attestation Service (NRAS).
// See https://docs.attestation.nvidia.com/api-docs/nras.html.
package nras

import (
	"log/slog"
	"net/http"
)

const (
	// URL is the URL of the NVIDIA Remote Attestation Service (NRAS).
	URL = "https://nras.attestation.nvidia.com"
	// Subject is the EAT subject of the NVIDIA Remote Attestation Service (NRAS).
	Subject = "NVIDIA-GPU-ATTESTATION"
)

// A Client talks to the NVIDIA Remote Attestation Service (NRAS).
// See https://docs.attestation.nvidia.com/api-docs/nras.html.
type Client struct {
	log    *slog.Logger
	client *http.Client
}

// NewClient creates a new NRAS client.
func NewClient(logger *slog.Logger) *Client {
	client := &http.Client{}
	return &Client{logger, client}
}

// Arch is the NVIDIA GPU architecture, as used by the NRAS.
type Arch string

const (
	// nrasArchAmpere is the NVIDIA Ampere architecture.
	_ Arch = "AMPERE"
	// ArchHopper is the NVIDIA ArchHopper architecture.
	ArchHopper Arch = "HOPPER"
)
