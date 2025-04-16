// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// main packages the native Desktop app for Continuum.
package main

import (
	"embed"
	"fmt"
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
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Failed to get user config dir:", err)
		os.Exit(1)
	}

	workspace := filepath.Join(cfgDir, "EdgelessSystems", "privatemode")
	log := logging.NewFileLogger("info", os.Stderr, filepath.Join(workspace, "log.txt"))

	app := NewApp(Config{
		Flags: setup.Flags{
			Workspace:      workspace,
			ManifestPath:   "",
			APIKey:         nil, // the key is set in the UI. needs to be nil
			APIEndpoint:    constants.APIEndpoint,
			SecretEndpoint: constants.SecretServiceEndpoint,
			ContrastFlags: setup.ContrastFlags{
				CoordinatorEndpoint: constants.CoordinatorEndpoint,
				CDNBaseURL:          "https://cdn.confidential.cloud/privatemode/v2",
			},
			InsecureAPIConnection: false,
		},
		runtimeConfig: jsonConfig{}, //nolint:exhaustruct
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
		Bind: []interface{}{
			&ConfigurationService{config: &app.config},
		},
	})
	if err != nil {
		log.Error("Failed to start the app", "error", err)
		os.Exit(1)
	}
}
