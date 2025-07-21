// Package mount handles mounting of model disks.
package mount

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/edgelesssys/continuum/disk-mounter/internal/mount/mount"
)

// VerityDisk mounts the verity partition of the given device or disk image.
func VerityDisk(ctx context.Context, devicePath, mountPath, rootHash string, log *slog.Logger) error {
	mounter, err := mount.New(log)
	if err != nil {
		return fmt.Errorf("creating mounter: %w", err)
	}

	blockDevice := false
	// Wait for disk to be available
	if err := retry.Do(
		func() error {
			blockDevice, err = isBlockDevice(devicePath)
			return err
		},
		retry.Delay(time.Second*30), // Wait for 30 seconds between each retry
		retry.OnRetry(func(n uint, err error) {
			log.Warn("Checking if disk is available", "attempt", n+1, "error", err)
		}),
	); err != nil {
		return fmt.Errorf("waiting for disk to be available: %w", err)
	}

	if blockDevice {
		if err := createDeviceNodes(ctx, devicePath, log); err != nil {
			return err
		}
	} else {
		// If the file at devicePath is not a block device, try to create a loopback device for it
		log.Info("Creating loopback device for disk image", "path", devicePath)
		devicePath, err = createLoopbackDevice(ctx, devicePath)
		if err != nil {
			return err
		}
	}

	stagingPath, err := mounter.MountDisk(ctx, devicePath, rootHash)
	if err != nil {
		return fmt.Errorf("mounting disk: %w", err)
	}

	if err := cleanTargetPath(mountPath); err != nil {
		return err
	}

	if err := os.Symlink(stagingPath, mountPath); err != nil {
		return fmt.Errorf("linking directory %q to %q: %w", stagingPath, mountPath, err)
	}

	return nil
}

// RemoveVerityDisk removes a verity disk from the system.
func RemoveVerityDisk(ctx context.Context, devicePath, mountPath string, log *slog.Logger) error {
	mounter, err := mount.New(log)
	if err != nil {
		return fmt.Errorf("creating mounter: %w", err)
	}
	return mounter.UnmountDisk(ctx, devicePath, mountPath)
}

func isBlockDevice(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("checking if %q is a block device: %w", path, err)
	}
	return (fi.Mode() & os.ModeDevice) != 0, nil
}

func createDeviceNodes(ctx context.Context, devicePath string, log *slog.Logger) error {
	lsblk, err := mount.NewLsblk()
	if err != nil {
		return fmt.Errorf("creating lsblk: %w", err)
	}
	device, err := lsblk.Device(ctx, devicePath)
	if err != nil {
		return err
	}
	if len(device.Children) != 2 {
		return fmt.Errorf("expected 2 partitions for device %q, got %d", devicePath, len(device.Children))
	}

	for _, child := range device.Children {
		_, err := os.Stat(child.Name)
		if err == nil {
			log.Info("Device node exists", "device", devicePath, "disk", device.Name, "partition", child.Name)
			continue
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("checking if device node %q exists: %w", child.Name, err)
		}

		// The Kernel knows about the partition, but the process can't access it
		// Let's create a device node for it
		log.Info("Creating device node for partition", "device", devicePath, "disk", device.Name, "partition", child.Name)
		if out, err := exec.CommandContext(ctx, "mknod", child.Name, "b", fmt.Sprint(child.Major), fmt.Sprint(child.Minor)).CombinedOutput(); err != nil {
			return fmt.Errorf("creating device node %q: %w: %s", child.Name, err, out)
		}
	}

	return nil
}

func createLoopbackDevice(ctx context.Context, diskImage string) (string, error) {
	cmd := exec.CommandContext(ctx, "losetup", "-f", "-L", "--show", "--partscan", diskImage)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("setting up loop device: %w: %s", err, output)
	}
	return strings.TrimSpace(string(output)), nil
}

// cleanTargetPath removes any existing symlinks at the given path.
// If the path exists and is not a symlink, an error is returned.
func cleanTargetPath(path string) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking if mount path %q exists: %w", path, err)
	}

	if fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("mount path %q exists and is not a symlink", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing existing mount path %q: %w", path, err)
	}

	return nil
}
