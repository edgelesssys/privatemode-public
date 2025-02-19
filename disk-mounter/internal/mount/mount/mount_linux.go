//go:build linux

package mount

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

type mountSyscall struct{}

func (m mountSyscall) mount(devicePath, mountPoint string) error {
	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		return fmt.Errorf("creating mount point %q: %w", mountPoint, err)
	}
	if err := unix.Mount(devicePath, mountPoint, "ext4", unix.MS_NODEV|unix.MS_NOSUID|unix.MS_RDONLY, ""); err != nil {
		return fmt.Errorf("mounting %q to %q: %w", devicePath, mountPoint, err)
	}
	return nil
}

func (m mountSyscall) unmount(mountPoint string) error {
	if err := unix.Unmount(mountPoint, 0); err != nil {
		return fmt.Errorf("unmounting %q: %w", mountPoint, err)
	}
	return nil
}
