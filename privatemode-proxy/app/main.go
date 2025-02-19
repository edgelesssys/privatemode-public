// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// main packages the native Desktop app for Continuum.
package main

import (
	"embed"
	"os"
	"path/filepath"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/logging"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/setup"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		panic(err)
	}
	workspace := filepath.Join(cfgDir, "EdgelessSystems", "privatemode")
	log := logging.NewFileLogger("info", os.Stderr, filepath.Join(workspace, "log.txt"))
	app := NewApp(Config{
		Flags: setup.Flags{
			Workspace:      workspace,
			ManifestPath:   "",
			SecretEndpoint: constants.SecretServiceEndpoint,
			ContrastFlags: setup.ContrastFlags{
				CoordinatorEndpoint:   constants.CoordinatorEndpoint,
				CoordinatorPolicyHash: "", // Only required when ManifestPath is set
				CDNBaseURL:            "https://cdn.confidential.cloud/privatemode/v2",
			},
		},
		APIEndpoint: constants.APIEndpoint,
	}, log)

	err = wails.Run(&options.App{ //nolint:exhaustruct
		Title:  "Privatemode AI",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Handler:    app.GetHandler(),
			Middleware: nil,
		},
		OnStartup:        app.OnStartup,
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
	})
	if err != nil {
		panic(err)
	}
}
