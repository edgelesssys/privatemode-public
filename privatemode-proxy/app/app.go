// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/server"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/setup"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const maxInitTimeout = 7 * time.Second // should normally take less than 5 seconds

// App wraps the server and handles deferred initialization.
type App struct {
	flags       setup.Flags
	log         *slog.Logger
	apiEndpoint string
	server      *server.Server
	initialized chan struct{}
}

// Config holds the configuration for the App.
type Config struct {
	Flags       setup.Flags
	APIEndpoint string
}

// NewApp creates a new App instance.
func NewApp(cfg Config, log *slog.Logger) *App {
	return &App{
		flags:       cfg.Flags,
		apiEndpoint: cfg.APIEndpoint,
		log:         log,
		server:      nil,
		initialized: make(chan struct{}),
	}
}

// OnStartup initializes the app when Wails starts.
func (a *App) OnStartup(ctx context.Context) {
	if err := a.initialize(ctx); err != nil {
		a.log.Error("Failed to initialize server", "error", err)
		action, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:          runtime.ErrorDialog,
			Title:         "Initialization Error",
			Message:       "The app could not be initialized with the remote service. Please check https://status.privatemode.ai and try again later. If problems persists, please contact support@privatemode.ai",
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
	manager, err := setup.SecretManager(ctx, a.flags, a.log)
	if err != nil {
		return fmt.Errorf("setting up secret manager: %w", err)
	}

	var apiKey *string // set key in the native app. needs to be nil
	a.server = server.New(http.DefaultClient, a.apiEndpoint, forwarder.SchemeHTTPS, manager, a.log, apiKey)
	close(a.initialized) // Signal that initialization is complete
	return nil
}
