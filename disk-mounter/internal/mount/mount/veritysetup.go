package mount

import (
	"context"
	"fmt"
	"os/exec"
)

type veritysetupCLI struct {
	path string
}

func (v veritysetupCLI) open(ctx context.Context, device, mappedDevice, hashDevice, rootHash string) error {
	if out, err := exec.CommandContext(
		ctx, v.path, "open",
		device, mappedDevice, hashDevice, rootHash,
	).CombinedOutput(); err != nil {
		return fmt.Errorf("opening dm-verity device: %w: %s", err, out)
	}
	return nil
}

func (v veritysetupCLI) close(ctx context.Context, device string) error {
	if out, err := exec.CommandContext(
		ctx, v.path, "close", device,
	).CombinedOutput(); err != nil {
		return fmt.Errorf("closing dm-verity device: %w: %s", err, out)
	}
	return nil
}
