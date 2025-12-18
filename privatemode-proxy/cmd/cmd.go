// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package cmd defines the privatemode-proxy's root command.
package cmd

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/setup"
	"github.com/spf13/cobra"
)

var (
	logLevel                     string
	logFormat                    string
	apiKeyStr                    string
	workspace                    string
	secretEndpoint               string
	apiEndpoint                  string
	port                         string
	manifestPath                 string
	nvidiaOCSPAllowUnknown       bool
	nvidiaOCSPRevokedGracePeriod int
	tlsCertPath                  string
	tlsKeyPath                   string
	insecureAPIConnection        bool
	dumpRequests                 bool

	// sharedPromptCache is used to share the cache between users.
	// When true, all users of the proxy will share the same cache.
	// When false (default), the proxy will disable caching by
	// using a random salt for each request. Clients can then
	// use request param cache_salt to enable caching.
	sharedPromptCache   bool
	promptCacheSalt     string
	coordinatorEndpoint string
	cdnBaseURL          string
)

// New returns the root command of the privatemode-proxy.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "privatemode-proxy",
		Short:   "The proxy verifies a third-party Privatemode deployment and handles prompt encryption and API authentication on behalf of its users.",
		Args:    cobra.NoArgs,
		Version: constants.Version(),
		PreRunE: func(_ *cobra.Command, _ []string) error {
			return logging.ValidateLogFormat(logFormat)
		},
		RunE:         runProxy,
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(&logLevel, logging.Flag, logging.FlagShorthand, logging.DefaultFlagValue, logging.FlagInfo)
	must(logging.RegisterFlagCompletionFunc(cmd))
	cmd.Flags().StringVar(&logFormat, logging.FormatFlag, logging.DefaultFormatFlagValue, logging.FormatFlagInfo)
	must(logging.RegisterFormatFlagCompletionFunc(cmd))

	cmd.Flags().StringVar(&apiKeyStr, "apiKey", "",
		"The API key for the Privatemode API. Accepts either a direct literal or a file path prefixed with '@'. If no key is set, the proxy will not authenticate with the API.")
	cmd.Flags().StringVar(&secretEndpoint, "ssEndpoint", constants.SecretServiceEndpoint, "The endpoint of the secret service.")
	cmd.Flags().StringVar(&apiEndpoint, "apiEndpoint", constants.APIEndpoint, "The endpoint for the Privatemode API")
	cmd.Flags().StringVar(&port, "port", "8080",
		"The port on which the proxy listens for incoming API requests.")
	cmd.Flags().StringVar(&workspace, "workspace", ".",
		fmt.Sprintf("The path into which the binary writes files. This includes the manifest log data in the '%s' subdirectory.", constants.ManifestDir))
	cmd.Flags().StringVar(&manifestPath, "manifestPath", "",
		"The path for the manifest file. If not provided, the manifest will be read from the remote source.")
	cmd.Flags().BoolVar(&nvidiaOCSPAllowUnknown, "nvidiaOCSPAllowUnknown", true,
		"Whether it should be tolerated if the NVIDIA OCSP service cannot be reached.")
	cmd.Flags().IntVar(&nvidiaOCSPRevokedGracePeriod, "nvidiaOCSPRevokedGracePeriod", 48,
		"The grace period (in hours) for which to accept NVIDIA attestation certificates that are revoked according to the OCSP service. "+
			"Supplying a value of 0 disables the grace period, meaning that revoked certificates are rejected immediately.")
	// prompt caching
	cmd.Flags().BoolVar(&sharedPromptCache, "sharedPromptCache", false,
		"If set, caching of prompts between all users of the proxy is enabled. This reduces response times for long conversations or common documents.")
	cmd.Flags().StringVar(&promptCacheSalt, "promptCacheSalt", "",
		"The salt used to isolate prompt caches. If empty (default), the same random salt is used for all requests, "+
			"enabling sharing the cache between all users of the same proxy. Requires 'sharedPromptCache' to be enabled!")

	cmd.Flags().BoolVar(&insecureAPIConnection, "insecureAPIConnection", false,
		"If set, the server will accept self-signed certificates from the API endpoint. Only intended for testing.")
	must(cmd.Flags().MarkHidden("insecureAPIConnection"))

	// TLS
	cmd.Flags().StringVar(&tlsCertPath, "tlsCertPath", "", "The path to the TLS certificate. If not provided, the server will start without TLS.")
	cmd.Flags().StringVar(&tlsKeyPath, "tlsKeyPath", "", "The path to the TLS key. If not provided, the server will start without TLS.")

	// Contrast flags
	cmd.Flags().StringVar(&coordinatorEndpoint, "coordinatorEndpoint", constants.CoordinatorEndpoint, "The endpoint for the Contrast coordinator.")
	cmd.Flags().StringVar(&cdnBaseURL, "cdnBaseURL", "https://cdn.confidential.cloud/privatemode/v2", "Base URL to retrieve deployment information from.")
	must(cmd.Flags().MarkHidden("cdnBaseURL"))

	// Request dumping
	cmd.Flags().BoolVar(&dumpRequests, "dumpRequests", false,
		"If set, the proxy dumps request and response logs to the '/requests' sub‑directory of the workspace. "+
			"Leaving this flag unset disables request and response dumping.")

	return cmd
}

func getPromptCacheSalt() (string, error) {
	if promptCacheSalt != "" && !sharedPromptCache {
		return "", fmt.Errorf("promptCacheSalt is set but sharedPromptCache is not enabled")
	}

	// if cache sharing is disabled, we must not use a salt but generate a random salt per-request
	if !sharedPromptCache {
		return "", nil
	}

	// if cache sharing is enabled, but no salt is set, we now generate a random salt
	// to keep for the lifetime of the proxy
	if promptCacheSalt == "" {
		return openai.RandomPromptCacheSalt()
	}

	return promptCacheSalt, nil
}

func runProxy(cmd *cobra.Command, _ []string) error {
	var log *slog.Logger
	if logFormat == logging.FormatFlagValueText {
		log = logging.NewCLILogger(logLevel, cmd.OutOrStderr())
	} else {
		log = logging.NewLogger(logLevel)
	}

	log.Info("Privatemode encryption proxy", "version", constants.Version())

	if (tlsCertPath == "") != (tlsKeyPath == "") {
		return errors.New("TLS certificate and key must be provided together")
	}

	cacheSalt, err := getPromptCacheSalt()
	if err != nil {
		return fmt.Errorf("getting prompt cache salt: %w", err)
	}

	var apiKey *string
	if cmd.Flags().Changed("apiKey") {
		if strings.HasPrefix(apiKeyStr, "@") {
			// Trim '@' and read file contents
			path := strings.TrimPrefix(apiKeyStr, "@")
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read API key file %q: %w", path, err)
			}
			key := strings.TrimSpace(string(data))
			apiKey = &key
		} else {
			// Direct literal
			apiKey = &apiKeyStr
		}
	} else {
		log.Warn("No API key provided. The proxy will not authenticate with the API.")
	}

	if !nvidiaOCSPAllowUnknown && (nvidiaOCSPRevokedGracePeriod > 0) {
		return errors.New("unknown OCSP statuses are disallowed, but revoked statuses are allowed. This is likely to be an erroneous configuration")
	}

	log.Info("Starting proxy")
	flags := setup.Flags{
		Workspace:      workspace,
		ManifestPath:   manifestPath,
		SecretEndpoint: secretEndpoint,
		ContrastFlags: setup.ContrastFlags{
			CoordinatorEndpoint: coordinatorEndpoint,
			CDNBaseURL:          cdnBaseURL,
		},
		InsecureAPIConnection:        insecureAPIConnection,
		APIEndpoint:                  apiEndpoint,
		APIKey:                       apiKey,
		PromptCacheSalt:              cacheSalt,
		NvidiaOCSPAllowUnknown:       nvidiaOCSPAllowUnknown,
		NvidiaOCSPRevokedGracePeriod: time.Duration(nvidiaOCSPRevokedGracePeriod) * time.Hour,
		// If request dumping is enabled, store dumps in a hard‑coded "/requests" sub‑directory
		// under the workspace. Otherwise leave the directory empty to disable dumping.
		DumpRequestsDir: func() string {
			if dumpRequests {
				return workspace + "/requests"
			}
			return ""
		}(),
	}
	manager, err := setup.SecretManager(cmd.Context(), flags, log)
	if err != nil {
		return fmt.Errorf("setting up secret manager configuration: %w", err)
	}
	const isApp = false

	lis, err := net.Listen("tcp", net.JoinHostPort("", port))
	if err != nil {
		return fmt.Errorf("listening on port %q: %w", port, err)
	}
	tlsConfig, err := getTLSConfig(tlsCertPath, tlsKeyPath)
	if err != nil {
		return fmt.Errorf("loading TLS config: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		loopLog := log.With("component", "secret-loop")
		if err := manager.Loop(cmd.Context(), loopLog); err != nil {
			loopLog.Error("Secret update loop exited", "error", err)
			// do not exit because the server will still keep the secrets up-to-date through incoming requests
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		err = setup.
			NewServer(flags, isApp, manager, log).
			Serve(cmd.Context(), lis, tlsConfig)
	}()

	wg.Wait()
	return err
}

// getTLSConfig returns the TLS configuration for production.
func getTLSConfig(tlsCertPath, tlsKeyPath string) (*tls.Config, error) {
	if tlsCertPath == "" && tlsKeyPath == "" {
		return nil, nil
	}
	return tlsFileReloadCfg(tlsCertPath, tlsKeyPath)
}

// tlsFileReloadCfg returns a [*tls.Config] that loads the certificate and key from the given paths for every connection. It validates the paths on creation.
func tlsFileReloadCfg(tlsCertPath, tlsKeyPath string) (*tls.Config, error) {
	getCert := func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		cert, err := tls.LoadX509KeyPair(tlsCertPath, tlsKeyPath)
		return &cert, err
	}
	if _, err := getCert(nil); err != nil {
		return nil, err
	}
	return &tls.Config{
		GetCertificate: getCert,
	}, nil
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
