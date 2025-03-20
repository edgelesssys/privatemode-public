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

	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/server"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/setup"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const maxInitTimeout = 7 * time.Second // should normally take less than 5 seconds

// Config holds the configuration for the App.
type Config struct {
	Flags       setup.Flags
	APIEndpoint string
	APIKey      string
}

// App wraps the server and handles deferred initialization.
type App struct {
	config      Config
	log         *slog.Logger
	server      *server.Server
	initialized chan struct{}
}

// NewApp creates a new App instance.
func NewApp(cfg Config, log *slog.Logger) *App {
	return &App{
		config:      cfg,
		log:         log,
		server:      nil,
		initialized: make(chan struct{}),
	}
}

// OnStartup initializes the app when Wails starts.
func (a *App) OnStartup(ctx context.Context) {
	configuredAPIKey, err := loadAPIKey(a.config.Flags.Workspace, a.log)
	if err != nil {
		a.log.Error("Failed to initialize server", "error", err)
		_, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:          runtime.ErrorDialog,
			Title:         "Configuration Error",
			Message:       fmt.Errorf("Error %w.\n\nPlease make sure the configuration file is correct. If the problem persists, please contact support@privatemode.ai", err).Error(),
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
	a.config.APIKey = configuredAPIKey

	if err := a.initialize(ctx); err != nil {
		a.log.Error("Failed to initialize server", "error", err)
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

// loadAPIKey returns no error when the file doesn't exist.
func loadAPIKey(workspace string, log *slog.Logger) (string, error) {
	configPath := filepath.Join(workspace, "config.json")
	_, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		log.Info("No configuration file found", "path", configPath)
		return "", nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("reading configuration file: %w", err)
	}

	var configFile struct {
		APIKey string `json:"app_key"`
	}
	if err := json.Unmarshal(data, &configFile); err != nil {
		return "", fmt.Errorf("parsing configuration file: %w", err)
	}

	if configFile.APIKey == "" {
		log.Info("API key not set in configuration file")
		return "", nil
	}

	return configFile.APIKey, nil
}

func (a *App) initialize(ctx context.Context) error {
	manager, err := setup.SecretManager(ctx, a.config.Flags, a.log)
	if err != nil {
		return fmt.Errorf("setting up secret manager: %w", err)
	}

	var apiKey *string // set key in the native app. needs to be nil
	a.server = server.New(http.DefaultClient, a.config.APIEndpoint, forwarder.SchemeHTTPS, manager, a.log, apiKey)
	close(a.initialized) // Signal that initialization is complete
	return nil
}
