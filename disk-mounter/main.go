// The disk-mounter handles mapping and mounting verity-protected disks.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/edgelesssys/continuum/disk-mounter/internal/mount"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/process"
)

func main() {
	devicePath := flag.String("device-path", "", "path to the device or disk image")
	mountPath := flag.String("mount-path", "", "path to mount the verity partition of the device to")
	rootHash := flag.String("root-hash", "", "root hash of the verity partition")
	logLevel := flag.String(logging.Flag, logging.DefaultFlagValue, logging.FlagInfo)
	flag.Parse()

	if *devicePath == "" || *mountPath == "" || *rootHash == "" {
		flag.Usage()
		os.Exit(1)
	}

	log := logging.NewLogger(*logLevel)
	log.Info("Continuum disk-mounter", "version", constants.Version())

	if err := run(*devicePath, *mountPath, *rootHash, log); err != nil {
		log.Error("Error running disk-mounter", "error", err)
		os.Exit(1)
	}
}

func run(devicePath, mountPath, rootHash string, log *slog.Logger) error {
	ctx, cancel := process.SignalContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := mount.VerityDisk(ctx, devicePath, mountPath, rootHash, log); err != nil {
		return fmt.Errorf("mounting verity disk: %w", err)
	}

	log.Info("Successfully performed workload setup. Waiting for termination signal...")
	<-ctx.Done()
	log.Info("Cleaning up device mounts")

	// Use a new context since the parent context was cancelled
	if err := mount.RemoveVerityDisk(context.Background(), devicePath, mountPath, log); err != nil {
		return fmt.Errorf("removing verity disk: %w", err)
	}

	log.Info("Successfully removed verity disk. Shutting down")
	return nil
}
