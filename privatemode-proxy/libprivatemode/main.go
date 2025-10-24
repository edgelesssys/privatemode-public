// package main implements a C-FFI-callable interface to start the privatemode-proxy server.
package main

import "C"

import (
	"context"
	"fmt"
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

	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return -1, fmt.Sprintf("getting user config dir: %s", err)
	}

	cacheSalt, err := openai.RandomPromptCacheSalt()
	if err != nil {
		return -1, fmt.Sprintf("generating random prompt cache salt: %s", err)
	}

	flags := setup.Flags{
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
		manager, err := setup.SecretManager(context.Background(), flags, log)
		if err != nil {
			log.Error("connecting to secret-service", "error", err)
			return
		}

		server := setup.NewServer(flags, true, manager, log)

		if err := server.Serve(context.Background(), listener, nil); err != nil {
			log.Error("running server", "error", err)
			return
		}
	}()

	return tcpAddr.Port, ""
}

func main() {}
