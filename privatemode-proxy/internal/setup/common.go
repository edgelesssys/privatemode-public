// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package setup defines the build specific setup for the privatemode-proxy.
package setup

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/httputil"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/server"
)

// Flags are flags that are common to all setups.
type Flags struct {
	ContrastFlags
	Workspace                    string
	ManifestPath                 string
	InsecureAPIConnection        bool
	APIEndpoint                  string
	APIKey                       *string
	DisableAPIKeyFallback        bool
	PromptCacheSalt              string
	NvidiaOCSPAllowUnknown       bool
	NvidiaOCSPRevokedGracePeriod time.Duration
	DumpRequestsDir              string
	CORSAllowedOrigins           []string
}

// ContrastFlags holds the configuration for the Contrast deployment.
type ContrastFlags struct {
	CDNBaseURL string
}

// NewServer creates a new server instance.
func NewServer(flags Flags, isApp bool, manager *secretmanager.SecretManager, log *slog.Logger) *server.Server {
	transport := httputil.NewTransport()
	// Non-streaming inference can be silent for more than ten minutes. Allow one hour,
	// consistent with the load balancer's tunnel timeout, without limiting response body duration.
	transport.ResponseHeaderTimeout = time.Hour
	if flags.InsecureAPIConnection {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	client := &http.Client{Transport: transport}

	opts := server.Opts{
		APIEndpoint:                  flags.APIEndpoint,
		APIKey:                       flags.APIKey,
		DisableAPIKeyFallback:        flags.DisableAPIKeyFallback,
		ProtocolScheme:               forwarder.SchemeHTTPS,
		PromptCacheSalt:              flags.PromptCacheSalt,
		IsApp:                        isApp,
		NvidiaOCSPAllowUnknown:       flags.NvidiaOCSPAllowUnknown,
		NvidiaOCSPRevokedGracePeriod: flags.NvidiaOCSPRevokedGracePeriod,
		DumpRequestsDir:              flags.DumpRequestsDir,
		CORSAllowedOrigins:           flags.CORSAllowedOrigins,
	}

	return server.New(client, manager, opts, log)
}
