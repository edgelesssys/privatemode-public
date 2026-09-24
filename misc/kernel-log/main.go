//go:build linux

// kernel-log streams the CVM kernel log to stdout for collection by Loki.
// Run inside the Kata guest with CAP_MKNOD and a writable /dev. Reading also
// requires dmesg_restrict=0 (otherwise CAP_SYSLOG is needed).
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"golang.org/x/sys/unix"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Our CVM base image has standard devices, but no /dev/kmsg by default.
	if err := unix.Mknod("/dev/kmsg", unix.S_IFCHR|0o400, int(unix.Mkdev(1, 11))); err != nil && !errors.Is(err, unix.EEXIST) {
		return fmt.Errorf("create /dev/kmsg: %w", err)
	}
	kmsg, err := os.Open("/dev/kmsg")
	if err != nil {
		return fmt.Errorf("open /dev/kmsg: %w", err)
	}
	defer kmsg.Close()

	return stream(kmsg, os.Stdout)
}

func stream(kmsg io.Reader, output io.Writer) error {
	// Each read must fit a whole record. Keep /dev/kmsg open: the kernel tracks
	// our read position per open file, so we read retained records once and
	// then block for new ones without polling.
	buf := make([]byte, 64*1024)
	for {
		n, err := kmsg.Read(buf)
		if n > 0 {
			if _, err := output.Write(buf[:n]); err != nil {
				return fmt.Errorf("write kernel log: %w", err)
			}
		}
		switch {
		case errors.Is(err, unix.EPIPE):
			// If logging outpaces us (e.g. stdout stalls), unread records can be
			// overwritten. The kernel advances our read position to the oldest
			// available record, so continue reading without reopening the file.
			log.Print("kernel-log: records lost due to kernel ring buffer overrun")
		case err == nil, errors.Is(err, unix.EINTR):
		case errors.Is(err, io.EOF):
			return nil
		default:
			return fmt.Errorf("read /dev/kmsg: %w", err)
		}
	}
}
