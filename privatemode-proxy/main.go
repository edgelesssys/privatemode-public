// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// main package of the privatemode-proxy.
package main

import (
	"context"
	"os"

	"github.com/edgelesssys/continuum/internal/gpl/process"
	"github.com/edgelesssys/continuum/privatemode-proxy/cmd"
)

func main() {
	ctx, cancel := process.SignalContext(context.Background(), os.Interrupt)
	defer cancel()

	cmd := cmd.New()
	if err := cmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
