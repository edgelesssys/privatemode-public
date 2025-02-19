// package mount implements safe mounting of dm-verity protected disks.
package mount

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/uuid"
)

const (
	veritySuffix = "-verity"
)

// Mounter mounts dm-verity protected disks.
type Mounter struct {
	veritysetup veritysetup
	mount       mount
	lsblk       lsblk

	log *slog.Logger
}

// New creates a new [Mounter].
func New(log *slog.Logger) (*Mounter, error) {
	veritysetupPath, mountPath, lsblkPath := "veritysetup", "mount", "lsblk"
	for _, path := range []string{veritysetupPath, mountPath, lsblkPath} {
		if _, err := exec.LookPath(path); err != nil {
			return nil, fmt.Errorf("checking for %s binary: %w", path, err)
		}
	}

	return &Mounter{
		veritysetup: veritysetupCLI{path: veritysetupPath},
		mount:       mountSyscall{},
		lsblk:       LsblkCLI{path: lsblkPath},
		log:         log,
	}, nil
}

// MountDisk mounts a verity protected disk using the given dm-verity rootHash,
// and returns the mount point.
// If no target mount point is provided, a random UUID is used to mount the disk at "/mnt/<uuid>".
// If the disk is already mounted, simply returns the existing mount point.
func (m *Mounter) MountDisk(ctx context.Context, devicePath, rootHash string) (string, error) {
	partitions, err := m.lsblk.name(ctx, devicePath)
	if err != nil {
		return "", err
	}
	slices.Sort(partitions)
	// Since we sort alphabetically and the device may be scsi or nvme,
	// the mapped device may be the first or the last partition
	// Ensure it is always the last partition for the following code
	if strings.HasPrefix(partitions[0], "/dev/mapper/") && len(partitions) > 1 {
		partitions = append(partitions[1:], partitions[0])
	}

	m.log.Info("Detected partitions", "partitions", partitions)

	switch len(partitions) {
	case 3:
		// The device has two partitions, but is not mounted or mapped
		// Expected output: <device> <partition1> <partition2>
		// --> Open the device and then mount it

		mappedDevice := verityDeviceName(partitions[0])
		m.log.Info("Opening verity partition", "device", devicePath, "dataDevice", partitions[1], "hashDevice", partitions[2], "rootHash", rootHash, "mappedDevice", mappedDevice)
		if err := m.veritysetup.open(ctx, partitions[1], mappedDevice, partitions[2], rootHash); err != nil {
			return "", err
		}

		return m.mountDisk(ctx, filepath.Join("/dev/mapper", mappedDevice))
	case 4:
		// The device has two partitions, and is mounted or mapped
		// Expected output: <device> <partition1> <partition2> <device>-verity
		// --> Verify the mapped device follows the expected naming pattern and then mount it

		m.log.Info("Device seems to already be mapped", "device", devicePath, "dataDevice", partitions[1], "hashDevice", partitions[2], "mappedDevice", partitions[3])
		if partitions[3] != filepath.Join("/dev/mapper", filepath.Base(partitions[0])+veritySuffix) {
			return "", fmt.Errorf("unexpected mapped device name: %q", partitions[3])
		}

		return m.mountDisk(ctx, partitions[3])
	default:
		return "", fmt.Errorf("unexpected number of partitions for device %q: %d: %v", devicePath, len(partitions), partitions)
	}
}

// UnmountDisk removes a verity protected disk from the system.
// It first unmounts the disk, then closes the verity device.
func (m *Mounter) UnmountDisk(ctx context.Context, devicePath, mountPoint string) error {
	m.log.Info("Unmounting device", "device", devicePath, "mountPoint", mountPoint)
	if err := m.mount.unmount(mountPoint); err != nil {
		return fmt.Errorf("unmounting device at path %q: %w", mountPoint, err)
	}

	device, err := m.lsblk.Device(ctx, devicePath)
	if err != nil {
		return fmt.Errorf("getting device information: %w", err)
	}

	if err := m.veritysetup.close(ctx, verityDeviceName(device.Name)); err != nil {
		return fmt.Errorf("closing verity device: %w", err)
	}
	return nil
}

// mountDisk mounts the given device at "/mnt/<uuid>", where "<uuid>" is a random UUID.
// If the device is already mounted, the function returns the existing mount point.
func (m *Mounter) mountDisk(ctx context.Context, devicePath string) (string, error) {
	// Check if device is already mounted
	mountPoint, err := m.lsblk.mountpoints(ctx, devicePath)
	if err != nil {
		return "", err
	}
	if mountPoint != "" {
		m.log.Info("Device is already mounted", "device", devicePath, "mountPoint", mountPoint)
		return mountPoint, nil
	}

	// Device is not mounted
	// Mount it at /mnt/<random-uid>
	mountPoint = fmt.Sprintf("/mnt/%s", uuid.New().String())
	m.log.Info("Mounting new device", "device", devicePath, "mountPoint", mountPoint)
	if err := m.mount.mount(devicePath, mountPoint); err != nil {
		return "", err
	}
	return mountPoint, nil
}

func verityDeviceName(devicePath string) string {
	return filepath.Base(devicePath) + veritySuffix
}

type mount interface {
	mount(devicePath, mountPoint string) error
	unmount(mountPoint string) error
}

type lsblk interface {
	Device(ctx context.Context, devicePath string) (BlockDevice, error)
	name(ctx context.Context, devicePath string) ([]string, error)
	mountpoints(ctx context.Context, devicePath string) (string, error)
}

type veritysetup interface {
	open(ctx context.Context, device, mappedDevice, hashDevice, rootHash string) error
	close(ctx context.Context, device string) error
}
