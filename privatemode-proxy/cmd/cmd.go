// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// package cmd defines the privatemode-proxy's root command.
package cmd

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/internal/gpl/logging"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/server"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/setup"
	"github.com/spf13/cobra"
)

var (
	logLevel                 string
	apiKeyStr                string
	workspace                string
	secretEndpoint           string
	apiEndpoint              string
	port                     string
	manifestPath             string
	tlsCertPath              string
	tlsKeyPath               string
	insecureAPIConnection    bool
	coordinatorEndpoint      string
	hexCoordinatorPolicyHash string
	cdnBaseURL               string
)

// New returns the root command of the privatemode-proxy.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "privatemode-proxy",
		Short:   "The proxy verifies a third-party Privatemode deployment and handles prompt encryption and API authentication on behalf of its users.",
		Args:    cobra.NoArgs,
		Version: constants.Version(),
		RunE:    runProxy,
	}

	cmd.Flags().StringVarP(&logLevel, logging.Flag, logging.FlagShorthand, logging.DefaultFlagValue, logging.FlagInfo)

	cmd.Flags().StringVar(&apiKeyStr, "apiKey", "", "The API key for the Privatemode API. If no key is set, the proxy will not authenticate with the API.")
	cmd.Flags().StringVar(&secretEndpoint, "ssEndpoint", constants.SecretServiceEndpoint, "The endpoint of the secret service.")
	cmd.Flags().StringVar(&apiEndpoint, "apiEndpoint", constants.APIEndpoint, "The endpoint for the Privatemode API")
	cmd.Flags().StringVar(&port, "port", "8080", "The port on which the proxy listens for incoming API requests.")
	cmd.Flags().StringVar(&workspace, "workspace", ".", fmt.Sprintf("The path into which the binary writes files. This includes the manifest log data in the '%s' subdirectory.", constants.ManifestDir))
	cmd.Flags().StringVar(&manifestPath, "manifestPath", "", "The path for the manifest file. If not provided, the manifest will be read from the remote source.")
	cmd.Flags().BoolVar(&insecureAPIConnection, "insecureAPIConnection", false, "If set, the server will accept self-signed certificates from the API endpoint. Only intended for testing.")
	must(cmd.Flags().MarkHidden("insecureAPIConnection"))

	// TLS
	cmd.Flags().StringVar(&tlsCertPath, "tlsCertPath", "", "The path to the TLS certificate. If not provided, the server will start without TLS.")
	cmd.Flags().StringVar(&tlsKeyPath, "tlsKeyPath", "", "The path to the TLS key. If not provided, the server will start without TLS.")

	// Contrast flags
	cmd.Flags().StringVar(&coordinatorEndpoint, "coordinatorEndpoint", constants.CoordinatorEndpoint, "The endpoint for the Contrast coordinator.")
	cmd.Flags().StringVar(&hexCoordinatorPolicyHash, "coordinatorPolicyHash", "", "The hex-encoded hash of the policy to be enforced by the coordinator.")
	cmd.MarkFlagsRequiredTogether("manifestPath", "coordinatorPolicyHash")
	cmd.Flags().StringVar(&cdnBaseURL, "cdnBaseURL", "https://cdn.confidential.cloud/privatemode/v2", "Base URL to retrieve deployment information from.")
	must(cmd.Flags().MarkHidden("cdnBaseURL"))

	return cmd
}

func runProxy(cmd *cobra.Command, _ []string) error {
	log := logging.NewLogger(logLevel)
	log.Info("Privatemode encryption proxy", "version", constants.Version())

	if (tlsCertPath == "") != (tlsKeyPath == "") {
		return errors.New("TLS certificate and key must be provided together")
	}

	var apiKey *string
	if cmd.Flags().Changed("apiKey") {
		apiKey = &apiKeyStr
	} else {
		log.Warn("No API key provided. The proxy will not authenticate with the API.")
	}

	log.Info("Starting proxy")
	flags := setup.Flags{
		Workspace:      workspace,
		ManifestPath:   manifestPath,
		SecretEndpoint: secretEndpoint,
		ContrastFlags: setup.ContrastFlags{
			CoordinatorEndpoint:   coordinatorEndpoint,
			CoordinatorPolicyHash: hexCoordinatorPolicyHash,
			CDNBaseURL:            cdnBaseURL,
		},
	}
	manager, err := setup.SecretManager(cmd.Context(), flags, log)
	if err != nil {
		return fmt.Errorf("setting up secret manager configuration: %w", err)
	}

	// setup server
	client := http.DefaultClient
	if insecureAPIConnection {
		client = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
	}
	server := server.New(client, apiEndpoint, forwarder.SchemeHTTPS, manager, log, apiKey)
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
		err = server.Serve(cmd.Context(), lis, tlsConfig)
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
