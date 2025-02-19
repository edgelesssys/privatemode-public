package mount

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// LsblkCLI is a wrapper around the lsblk command line tool.
type LsblkCLI struct {
	path string
}

// NewLsblk creates a new LsblkCLI.
func NewLsblk() (LsblkCLI, error) {
	path := "lsblk"
	if _, err := exec.LookPath(path); err != nil {
		return LsblkCLI{}, fmt.Errorf("checking for %q binary: %w", path, err)
	}
	return LsblkCLI{path: path}, nil
}

// Device returns information about the block device at the given path.
func (l LsblkCLI) Device(ctx context.Context, devicePath string) (BlockDevice, error) {
	out, err := exec.CommandContext(ctx, l.path, "--json", "--path", devicePath).CombinedOutput()
	if err != nil {
		return BlockDevice{}, fmt.Errorf("getting device %q: %w: %s", devicePath, err, out)
	}
	var devices struct {
		Blockdevices []BlockDevice
	}
	if err := json.Unmarshal(out, &devices); err != nil {
		return BlockDevice{}, fmt.Errorf("unmarshaling block device: %w", err)
	}

	var blkDev BlockDevice
	if len(devices.Blockdevices) < 1 {
		return blkDev, fmt.Errorf("device %q not found", devicePath)
	}
	return devices.Blockdevices[0], nil
}

func (l LsblkCLI) name(ctx context.Context, devicePath string) ([]string, error) {
	out, err := exec.CommandContext(ctx, l.path, "--output", "name", "--list", "--path", "--noheadings", devicePath).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("getting partitions of %q: %w: %s", devicePath, err, out)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n"), nil
}

func (l LsblkCLI) mountpoints(ctx context.Context, devicePath string) (string, error) {
	out, err := exec.CommandContext(ctx, l.path, "--output", "mountpoints", "--list", "--path", "--noheadings", devicePath).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("getting mount points of %q: %w: %s", devicePath, err, out)
	}
	mountPoint, _, _ := strings.Cut(string(out), "\n") // If there are multiple mount points, only return the first one
	return mountPoint, nil
}

// BlockDevice represents a block device.
type BlockDevice struct {
	Name        string
	Major       string
	Minor       string
	Size        string
	Type        string
	RM          bool
	RO          bool
	MountPoints []string
	Children    []BlockDevice
}

// UnmarshalJSON unmarshals the block device from JSON.
func (b *BlockDevice) UnmarshalJSON(data []byte) error {
	var blkDev struct {
		Name        string        `json:"name"`
		MajorMinor  string        `json:"maj:min"`
		Size        string        `json:"size"`
		Type        string        `json:"type"`
		RM          bool          `json:"rm"`
		RO          bool          `json:"ro"`
		MountPoints []string      `json:"mountpoints,omitempty"`
		Children    []BlockDevice `json:"children,omitempty"`
	}
	if err := json.Unmarshal(data, &blkDev); err != nil {
		return err
	}
	major, minor, _ := strings.Cut(blkDev.MajorMinor, ":")
	b.Major = major
	b.Minor = minor
	b.Name = blkDev.Name
	b.Size = blkDev.Size
	b.Type = blkDev.Type
	b.RM = blkDev.RM
	b.RO = blkDev.RO
	b.MountPoints = blkDev.MountPoints
	b.Children = blkDev.Children
	return nil
}
