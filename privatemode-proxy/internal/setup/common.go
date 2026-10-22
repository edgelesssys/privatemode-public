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
	Workspace                     string
	ManifestPath                  string
	InsecureAPIConnection         bool
	APIEndpoint                   string
	APIKey                        *string
	APIKeyFallback                server.APIKeyFallback
	TimeoutRead                   time.Duration
	TimeoutReadHeader             time.Duration
	TimeoutWrite                  time.Duration
	TimeoutIdle                   time.Duration
	TimeoutUpstreamResponseHeader time.Duration
	PromptCacheSalt               string
	PromptCacheSaltFromAPIKey     bool
	NvidiaOCSPAllowUnknown        bool
	NvidiaOCSPRevokedGracePeriod  time.Duration
	DumpRequestsDir               string
	CORSAllowedOrigins            []string
	ClientID                      string
	ModelsDocument                *server.ModelsDocument
}

// ContrastFlags holds the configuration for the Contrast deployment.
type ContrastFlags struct {
	CDNBaseURL string
}

// NewServer creates a new server instance.
func NewServer(flags Flags, manager *secretmanager.SecretManager, log *slog.Logger) *server.Server {
	transport := httputil.NewTransport()
	transport.ResponseHeaderTimeout = flags.TimeoutUpstreamResponseHeader
	if flags.InsecureAPIConnection {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	client := &http.Client{Transport: transport}

	opts := server.Opts{
		APIEndpoint:                  flags.APIEndpoint,
		APIKey:                       flags.APIKey,
		APIKeyFallback:               flags.APIKeyFallback,
		ProtocolScheme:               forwarder.SchemeHTTPS,
		TimeoutRead:                  flags.TimeoutRead,
		TimeoutReadHeader:            flags.TimeoutReadHeader,
		TimeoutWrite:                 flags.TimeoutWrite,
		TimeoutIdle:                  flags.TimeoutIdle,
		PromptCacheSalt:              flags.PromptCacheSalt,
		PromptCacheSaltFromAPIKey:    flags.PromptCacheSaltFromAPIKey,
		NvidiaOCSPAllowUnknown:       flags.NvidiaOCSPAllowUnknown,
		NvidiaOCSPRevokedGracePeriod: flags.NvidiaOCSPRevokedGracePeriod,
		DumpRequestsDir:              flags.DumpRequestsDir,
		CORSAllowedOrigins:           flags.CORSAllowedOrigins,
		ClientID:                     flags.ClientID,
		ModelsDocument:               flags.ModelsDocument,
	}

	return server.New(client, manager, opts, log)
}
