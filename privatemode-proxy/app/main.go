// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// main packages the native Desktop app for Continuum.
package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/logging"
	"github.com/edgelesssys/continuum/internal/gpl/openai"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/setup"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

func main() {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Failed to get user config dir:", err)
		os.Exit(1)
	}

	workspace := filepath.Join(cfgDir, "EdgelessSystems", "privatemode")
	log := logging.NewFileLogger("info", os.Stderr, filepath.Join(workspace, "log.txt"))

	cacheSalt, err := openai.RandomPromptCacheSalt()
	if err != nil {
		log.Error("Failed to generate random salt for prompt caching", "error", err)
		os.Exit(1)
	}

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
			// In the app we always want prompt caching and use a random salt that lives as long as the app.
			// This may be overridden in the config file to enable cache sharing between users.
			PromptCacheSalt:              cacheSalt,
			NvidiaOCSPAllowUnknown:       true,           // TODO(msanft): make this configurable
			NvidiaOCSPRevokedGracePeriod: 48 * time.Hour, // TODO(msanft): make this configurable
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
		Mac: &mac.Options{ //nolint:exhaustruct
			About: &mac.AboutInfo{
				Title:   "About Privatemode AI",
				Message: fmt.Sprintf("Version %s\n© 2025 Edgeless Systems GmbH", constants.Version()),
				Icon:    icon,
			},
		},
		Bind: []interface{}{
			&ConfigurationService{config: &app.config},
			&SmokeTestService{app: app},
		},
	})
	if err != nil {
		log.Error("Failed to start the app", "error", err)
		os.Exit(1)
	}
}
