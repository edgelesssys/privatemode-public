// Package policy contains policy definitions for GPU attestation
package policy

// NvidiaHopper defines the allowed configuration for an NVIDIA Hopper GPU used by worker nodes.
type NvidiaHopper struct {
	// Debug specifies whether the GPU is allowed to run in CC debug mode.
	// Setting this to true will allow both debug and non-debug enabled GPUs.
	Debug bool `json:"debug" toml:"debug" comment:"allow the GPU to run in CC debug mode"`
	// SecureBoot specifies if the GPU is required to run with secure boot enabled.
	SecureBoot bool `json:"secureBoot" toml:"secureBoot" comment:"require the GPU to run with secure boot enabled"`
	// EATVersion specifies the expected EAT version.
	EATVersion string `json:"eatVersion" toml:"eatVersion" comment:"expected EAT version"`

	// TODO: Decide how we want to appraise versions for GPUs
	// Instead of defining multiple lists for all versions,
	// we could define one NvidiaHopper policy per driver version, BIOS version etc.
	// Or allow defining multiple driver versions, but just one BIOS version.
	// Keeping it like this makes the policy more flexible and reduces the number of policies,
	// but it is not as fine grained.

	MismatchingMeasurements []int `json:"mismatchingMeasurements" toml:"mismatchingMeasurements" comment:"allow mismatching GPU measurements for these indices"`
	// DriverVersions is a list of allowed driver versions for the GPU.
	DriverVersions []string `json:"driverVersions" toml:"driverVersions" comment:"allowed driver versions for the GPU"`
	// VBIOSVersions is a list of allowed vBIOS versions for the GPU.
	VBIOSVersions []string `json:"vbiosVersions" toml:"vbiosVersions" comment:"allowed vBIOS versions for the GPU"`
}
