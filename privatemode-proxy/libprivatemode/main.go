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
	"time"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/logging"
	"github.com/edgelesssys/continuum/internal/gpl/openai"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/setup"
)

//export PrivatemodeStartProxy
func PrivatemodeStartProxy() (int, string) {
	log := logging.NewCLILogger("info", os.Stderr)
	log.Info("Starting privatemode-proxy")

	flags, err := flagsFromEnv(log)
	if err != nil {
		return -1, fmt.Sprintf("getting flags from env: %s", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:")
	if err != nil {
		return -1, fmt.Sprintf("setting up listener: %s", err)
	}

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return -1, "getting listener address: not a TCP address"
	}

	go func() {
		log.Info("Connecting to secret-service")
		manager, err := setup.SecretManager(context.Background(), *flags, log)
		if err != nil {
			log.Error("connecting to secret-service", "error", err)
			return
		}

		server := setup.NewServer(*flags, true, manager, log)

		if err := server.Serve(context.Background(), listener, nil); err != nil {
			log.Error("running server", "error", err)
			return
		}
	}()

	return tcpAddr.Port, ""
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
		Workspace:      filepath.Join(cfgDir, "EdgelessSystems", "privatemode"),
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
	}

	if apiEndpoint := os.Getenv("LIBPRIVATEMODE_API_ENDPOINT"); apiEndpoint != "" {
		log.Info("LIBPRIVATEMODE_API_ENDPOINT is set, overriding default API endpoint", "endpoint", apiEndpoint)
		flags.APIEndpoint = apiEndpoint
		flags.InsecureAPIConnection = true
	}

	if secretEndpoint := os.Getenv("LIBPRIVATEMODE_SECRET_ENDPOINT"); secretEndpoint != "" {
		log.Info("LIBPRIVATEMODE_SECRET_ENDPOINT is set, overriding default secret endpoint", "endpoint", secretEndpoint)
		flags.SecretEndpoint = secretEndpoint
	}

	if coordinatorEndpoint := os.Getenv("LIBPRIVATEMODE_COORDINATOR_ENDPOINT"); coordinatorEndpoint != "" {
		log.Info("LIBPRIVATEMODE_COORDINATOR_ENDPOINT is set, overriding default coordinator endpoint", "endpoint", coordinatorEndpoint)
		flags.CoordinatorEndpoint = coordinatorEndpoint
	}

	if manifestPath := os.Getenv("LIBPRIVATEMODE_MANIFEST_PATH"); manifestPath != "" {
		log.Info("LIBPRIVATEMODE_MANIFEST_PATH is set, overriding default manifest path", "path", manifestPath)
		flags.ManifestPath = manifestPath
	}

	return flags, nil
}

func main() {}
