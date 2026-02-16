// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// package main implements a C-FFI-callable interface to start the privatemode-proxy server.
package main

import "C"

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/setup"
)

var (
	currentManifest   = func() string { return "" }
	currentManifestMu sync.Mutex
)

//export PrivatemodeStartProxy
func PrivatemodeStartProxy(dataDir *C.char) (int, *C.char) {
	log := logging.NewCLILogger("info", os.Stderr)
	log.Info("Starting privatemode-proxy")

	// On Android, HOME and XDG_CONFIG_HOME are not set. The caller passes the
	// app's files directory so Go's os.UserConfigDir() can resolve a path.
	// C's setenv() does not work because Go caches the environment at init.
	if dataDir != nil {
		dir := C.GoString(dataDir)
		if dir != "" {
			if os.Getenv("HOME") == "" {
				os.Setenv("HOME", dir)
			}
			if os.Getenv("XDG_CONFIG_HOME") == "" {
				os.Setenv("XDG_CONFIG_HOME", dir)
			}
		}
	}

	flags, err := flagsFromEnv(log)
	if err != nil {
		return -1, C.CString(fmt.Sprintf("getting flags from env: %s", err))
	}

	listener, err := net.Listen("tcp", "127.0.0.1:")
	if err != nil {
		return -1, C.CString(fmt.Sprintf("setting up listener: %s", err))
	}

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return -1, C.CString("getting listener address: not a TCP address")
	}

	go func() {
		log.Info("Connecting to secret-service")
		manager, getManifest, err := setup.SecretManager(context.Background(), *flags, log)
		if err != nil {
			log.Error("connecting to secret-service", "error", err)
			return
		}
		currentManifestMu.Lock()
		currentManifest = getManifest
		currentManifestMu.Unlock()

		server := setup.NewServer(*flags, true, manager, log)

		if err := server.Serve(context.Background(), listener, nil); err != nil {
			log.Error("running server", "error", err)
			return
		}
	}()

	return tcpAddr.Port, nil
}

//export CurrentManifest
func CurrentManifest() *C.char {
	currentManifestMu.Lock()
	defer currentManifestMu.Unlock()
	return C.CString(currentManifest())
}

func flagsFromEnv(log *slog.Logger) (*setup.Flags, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("getting user config dir: %w", err)
	}

	cacheSalt, err := openai.RandomPromptCacheSalt()
	if err != nil {
		return nil, fmt.Errorf("generating random prompt cache salt: %w", err)
	}

	flags := &setup.Flags{
		Workspace:    filepath.Join(cfgDir, "EdgelessSystems", "privatemode"),
		ManifestPath: "",
		APIKey:       nil, // the key is set in the UI. needs to be nil
		APIEndpoint:  constants.APIEndpoint,
		ContrastFlags: setup.ContrastFlags{
			CDNBaseURL: "https://cdn.confidential.cloud/privatemode/v2",
		},
		InsecureAPIConnection: false,
		// In the app we always want prompt caching and use a random salt that lives as long as the app.
		// This may be overridden in the config file to enable cache sharing between users.
		PromptCacheSalt:              cacheSalt,
		NvidiaOCSPAllowUnknown:       true,           // TODO(msanft): make this configurable
		NvidiaOCSPRevokedGracePeriod: 48 * time.Hour, // TODO(msanft): make this configurable
	}

	if apiEndpoint := os.Getenv("LIBPRIVATEMODE_API_ENDPOINT"); apiEndpoint != "" {
		log.Info("LIBPRIVATEMODE_API_ENDPOINT is set, overriding default API endpoint", "endpoint", apiEndpoint)
		flags.APIEndpoint = apiEndpoint
		flags.InsecureAPIConnection = true
	}

	if manifestPath := os.Getenv("LIBPRIVATEMODE_MANIFEST_PATH"); manifestPath != "" {
		log.Info("LIBPRIVATEMODE_MANIFEST_PATH is set, overriding default manifest path", "path", manifestPath)
		flags.ManifestPath = manifestPath
	}

	return flags, nil
}

func main() {}
