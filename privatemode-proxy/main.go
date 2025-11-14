// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// main package of the privatemode-proxy.
package main

import (
	"context"
	"os"

	"github.com/edgelesssys/continuum/internal/oss/process"
	"github.com/edgelesssys/continuum/privatemode-proxy/cmd"
)

func main() {
	if err := execute(); err != nil {
		os.Exit(1)
	}
}

func execute() error {
	ctx, cancel := process.SignalContext(context.Background(), os.Interrupt)
	defer cancel()

	cmd := cmd.New()
	return cmd.ExecuteContext(ctx)
}
