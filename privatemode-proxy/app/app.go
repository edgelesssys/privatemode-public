// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/server"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/setup"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const maxInitTimeout = 7 * time.Second // should normally take less than 5 seconds

// Config holds the configuration for the App.
type Config struct {
	Flags         setup.Flags
	runtimeConfig jsonConfig
}

// Update updates the configuration based on the JSON config file.
func (c *Config) Update(log *slog.Logger) error {
	runtimeConfig, err := loadRuntimeConfig(c.Flags.Workspace, log)
	if err != nil {
		return fmt.Errorf("loading runtime config: %w", err)
	}
	c.runtimeConfig = runtimeConfig
	c.update(runtimeConfig)
	return nil
}

// GetConfiguredAPIKey returns the configured API key.
func (c *Config) GetConfiguredAPIKey() string {
	return c.runtimeConfig.AccessKey
}

func (c *Config) update(runtimeConfig jsonConfig) {
	c.Flags.APIEndpoint = addPrefix(constants.APIEndpoint, runtimeConfig.DeploymentUID)
	c.Flags.SecretEndpoint = addPrefix(constants.SecretServiceEndpoint, runtimeConfig.DeploymentUID)
	c.Flags.CoordinatorEndpoint = addPrefix(constants.CoordinatorEndpoint, runtimeConfig.DeploymentUID)
	c.Flags.InsecureAPIConnection = runtimeConfig.DeploymentUID != ""
	c.Flags.ManifestPath = runtimeConfig.ManifestPath

	if runtimeConfig.PromptCacheSalt != "" {
		c.Flags.PromptCacheSalt = runtimeConfig.PromptCacheSalt
	}
}

// App wraps the server and handles deferred initialization.
type App struct {
	ctx         context.Context
	config      Config
	log         *slog.Logger
	server      *server.Server
	initialized chan struct{}
}

// NewApp creates a new App instance.
func NewApp(cfg Config, log *slog.Logger) *App {
	return &App{
		ctx:         nil,
		config:      cfg,
		log:         log,
		server:      nil,
		initialized: make(chan struct{}),
	}
}

// OnStartup initializes the app when Wails starts.
func (a *App) OnStartup(ctx context.Context) {
	a.ctx = ctx
	if err := a.config.Update(a.log); err != nil {
		a.log.Error("Loading JSON configuration", "error", err)
		_, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:          runtime.ErrorDialog,
			Title:         "Configuration Error",
			Message:       fmt.Errorf("Error %w.\n\nPlease make sure the configuration file is correct. If the problem persists, please contact support@privatemode.ai", err).Error(), //nolint:staticcheck
			Buttons:       []string{"Close"},
			DefaultButton: "CLose",
			CancelButton:  "Close",
			Icon:          nil,
		})
		if err != nil {
			a.log.Error("Failed to show error dialog", "error", err)
		}
		runtime.Quit(ctx)
		return
	}

	if err := a.initialize(ctx); err != nil {
		a.log.Error("Initializing server", "error", err)
		action, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:          runtime.ErrorDialog,
			Title:         "Initialization Error",
			Message:       "The app could not be initialized with the remote service. Please check https://status.privatemode.ai and try again later. If the problem persists, please contact support@privatemode.ai",
			Buttons:       []string{"Retry", "Close"},
			DefaultButton: "Retry",
			CancelButton:  "Close",
			Icon:          nil,
		})
		if err != nil {
			a.log.Error("Failed to show dialog", "error", err)
			runtime.Quit(ctx)
			return
		}

		switch action {
		case "Close":
			runtime.Quit(ctx)
		case "Retry":
			a.OnStartup(ctx)
		}
	}
}

// GetHandler returns an http.Handler that checks initialization status.
func (a *App) GetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), maxInitTimeout)
		defer cancel()

		select {
		case <-ctx.Done():
			http.Error(w, "Timed out waiting for initialization to complete. Please retry in a few seconds.", http.StatusServiceUnavailable)
		case <-a.initialized:
			a.server.GetHandler().ServeHTTP(w, r)
		}
	})
}

func (a *App) initialize(ctx context.Context) error {
	manager, err := setup.SecretManager(ctx, a.config.Flags, a.log)
	if err != nil {
		return fmt.Errorf("setting up secret manager: %w", err)
	}

	const isApp = true
	a.server = setup.NewServer(a.config.Flags, isApp, manager, a.log)
	close(a.initialized) // Signal that initialization is complete
	return nil
}

type jsonConfig struct {
	AccessKey       string `json:"access_key"`
	PromptCacheSalt string `json:"prompt_cache_salt"`
	// Dev-only
	DeploymentUID string `json:"deployment_uid"`
	ManifestPath  string `json:"manifest_path"`
}

func addPrefix(endpoint, deploymentUID string) string {
	if deploymentUID == "" {
		return endpoint
	}
	return deploymentUID + "." + endpoint
}

// loadRuntimeConfig returns no error when the file doesn't exist.
func loadRuntimeConfig(workspace string, log *slog.Logger) (jsonConfig, error) {
	configPath := filepath.Join(workspace, "config.json")
	_, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		log.Info("No configuration file found", "path", configPath)
		return jsonConfig{}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return jsonConfig{}, fmt.Errorf("reading configuration file: %w", err)
	}

	var configFile jsonConfig
	if err := json.Unmarshal(data, &configFile); err != nil {
		return jsonConfig{}, fmt.Errorf("parsing configuration file: %w", err)
	}

	if configFile.AccessKey == "" {
		log.Info("Access key not set in configuration file")
	}
	if configFile.PromptCacheSalt != "" {
		log.Info("PromptCacheSalt set in configuration file")
	}

	return configFile, nil
}
