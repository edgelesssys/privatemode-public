// The disk-mounter handles mapping and mounting verity-protected disks.
package main

import (
	"context"
	"flag"
	"os"

	"github.com/edgelesssys/continuum/disk-mounter/internal/mount"
	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/logging"
	"github.com/edgelesssys/continuum/internal/gpl/process"
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

	ctx, cancel := process.SignalContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := mount.VerityDisk(ctx, *devicePath, *mountPath, *rootHash, log); err != nil {
		log.Error("Failed to mount verity disk", "error", err)
		os.Exit(1)
	}

	log.Info("Successfully performed workload setup. Waiting for termination signal...")
	<-ctx.Done()
	log.Info("Cleaning up device mounts")

	// Use a new context since the parent context was cancelled
	if err := mount.RemoveVerityDisk(context.Background(), *devicePath, *mountPath, log); err != nil {
		log.Error("Failed to remove verity disk", "error", err)
		os.Exit(1)
	}

	log.Info("Successfully removed verity disk. Shutting down")
}
