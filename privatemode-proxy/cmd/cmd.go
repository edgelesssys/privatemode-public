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
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/tlsconfig"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/server"
	"github.com/edgelesssys/continuum/privatemode-proxy/internal/setup"
	"github.com/spf13/cobra"
)

// clientIDPattern restricts --client-id to characters that are safe in a header value.
var clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

var (
	logLevel                     string
	logFormat                    string
	apiKeyStr                    string
	apiKeyFallback               string
	workspace                    string
	apiEndpoint                  string
	listenAddress                string
	port                         string
	manifestPath                 string
	nvidiaOCSPAllowUnknown       bool
	nvidiaOCSPRevokedGracePeriod int
	tlsCertPath                  string
	tlsKeyPath                   string
	insecureAPIConnection        bool
	dumpRequests                 bool
	corsAllowedOrigins           []string
	clientID                     string
	modelsDocumentPath           string

	disablePromptCache    bool
	promptCacheSalt       string
	sharedPromptCache     bool
	legacyPromptCacheSalt string
	cdnBaseURL            string
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

	cmd.Flags().StringVar(&apiKeyStr, "api-key", "",
		"The API key for the Privatemode API. Accepts either a direct literal or a file path prefixed with '@'. If no key is set, the proxy will not authenticate with the API.")
	cmd.Flags().StringVar(&apiKeyFallback, "api-key-fallback", string(server.APIKeyFallbackAlways),
		"When to use the configured API key for forwarded requests: never, always, or if-placeholder (only when the API key in the request is 'placeholder'). The default will change to never in a future release.")
	must(cmd.RegisterFlagCompletionFunc("api-key-fallback", cobra.FixedCompletions([]string{
		string(server.APIKeyFallbackNever), string(server.APIKeyFallbackAlways), string(server.APIKeyFallbackIfPlaceholder),
	}, cobra.ShellCompDirectiveNoFileComp)))
	cmd.Flags().StringVar(&apiEndpoint, "api-endpoint", constants.APIEndpoint, "The endpoint for the Privatemode API")
	cmd.Flags().StringVar(&listenAddress, "listen-address", "0.0.0.0",
		"The IP address or hostname on which the proxy listens for incoming API requests.")
	cmd.Flags().StringVar(&port, "port", "8080",
		"The port on which the proxy listens for incoming API requests.")
	cmd.Flags().StringSliceVar(&corsAllowedOrigins, "cors-allowed-origins", nil,
		"Allowed browser origins (comma-separated). Empty disables CORS; '*' allows any origin.")
	cmd.Flags().StringVar(&workspace, "workspace", ".",
		fmt.Sprintf("The path into which the binary writes files. This includes the manifest log data in the '%s' subdirectory.", constants.ManifestDir))
	cmd.Flags().StringVar(&manifestPath, "manifest-path", "",
		"The path for the manifest file. If not provided, the manifest will be read from the remote source.")
	cmd.Flags().BoolVar(&nvidiaOCSPAllowUnknown, "nvidia-ocsp-allow-unknown", true,
		"Whether it should be tolerated if the NVIDIA OCSP service cannot be reached.")
	cmd.Flags().IntVar(&nvidiaOCSPRevokedGracePeriod, "nvidia-ocsp-revoked-grace-period", 48,
		"The grace period (in hours) for which to accept NVIDIA attestation certificates that are revoked according to the OCSP service. "+
			"Supplying a value of 0 disables the grace period, meaning that revoked certificates are rejected immediately.")
	// prompt caching
	cmd.Flags().BoolVar(&disablePromptCache, "disable-prompt-cache", true,
		"Use a new random salt per request instead of sharing a cache across the proxy. Request cache_salt can still be used to achieve shared caches.")
	cmd.Flags().StringVar(&promptCacheSalt, "prompt-cache-salt", "random",
		"The salt used to separate prompt caches. Can either be a literal, @file, or random (one salt per proxy startup). Requires --disable-prompt-cache=false. Request cache_salt takes precedence. Literal and file salts must be valid UTF-8, at least 32 bytes and at most 128 characters, without '@', '/', '\\', or NUL.")
	cmd.Flags().BoolVar(&sharedPromptCache, "sharedPromptCache", false,
		"Share a prompt cache between users of the proxy. Request cache_salt takes precedence.")
	cmd.Flags().StringVar(&legacyPromptCacheSalt, "promptCacheSalt", "",
		"Shared cache salt: a literal of at least 32 bytes or @file. Requires --sharedPromptCache. If empty, generate one salt for the proxy lifetime.")
	must(cmd.Flags().MarkDeprecated("sharedPromptCache", "use --disable-prompt-cache=false to enable sharing"))
	must(cmd.Flags().MarkDeprecated("promptCacheSalt", "use --prompt-cache-salt instead"))
	cmd.MarkFlagsMutuallyExclusive("disable-prompt-cache", "sharedPromptCache")
	cmd.MarkFlagsMutuallyExclusive("disable-prompt-cache", "promptCacheSalt")
	cmd.MarkFlagsMutuallyExclusive("prompt-cache-salt", "sharedPromptCache")
	cmd.MarkFlagsMutuallyExclusive("prompt-cache-salt", "promptCacheSalt")

	cmd.Flags().BoolVar(&insecureAPIConnection, "insecure-api-connection", false,
		"If set, the server will accept self-signed certificates from the API endpoint. Only intended for testing.")
	must(cmd.Flags().MarkHidden("insecure-api-connection"))

	// TLS
	cmd.Flags().StringVar(&tlsCertPath, "tls-cert-path", "", "The path to the TLS certificate. If not provided, the server will start without TLS.")
	cmd.Flags().StringVar(&tlsKeyPath, "tls-key-path", "", "The path to the TLS key. If not provided, the server will start without TLS.")

	// Contrast flags
	cmd.Flags().StringVar(&cdnBaseURL, "cdn-base-url", "https://cdn.confidential.cloud/privatemode/v2", "Base URL to retrieve deployment information from.")
	must(cmd.Flags().MarkHidden("cdn-base-url"))

	// Hosted deployments
	cmd.Flags().StringVar(&clientID, "client-id", "",
		"Value of the Privatemode-Client header sent to the API. Identifies a hosted proxy deployment in the API logs.")
	must(cmd.Flags().MarkHidden("client-id"))
	cmd.Flags().StringVar(&modelsDocumentPath, "models-document", "",
		"Path to a JSON document served at /v1/models instead of forwarding the request. Re-read when the file changes.")
	must(cmd.Flags().MarkHidden("models-document"))

	// Request dumping
	cmd.Flags().BoolVar(&dumpRequests, "dump-requests", false,
		"If set, the proxy dumps request and response logs to the '/requests' sub‑directory of the workspace. "+
			"Leaving this flag unset disables request and response dumping.")

	// Keep camelCase aliases for one release. Share the Value so repeated flags,
	// including slices and mixed spellings, retain the usual parsing behavior.
	// TODO(PM-489): Remove these aliases after the compatibility release.
	for oldName, name := range map[string]string{
		"apiKey":                       "api-key",
		"apiEndpoint":                  "api-endpoint",
		"corsAllowedOrigins":           "cors-allowed-origins",
		"manifestPath":                 "manifest-path",
		"nvidiaOCSPAllowUnknown":       "nvidia-ocsp-allow-unknown",
		"nvidiaOCSPRevokedGracePeriod": "nvidia-ocsp-revoked-grace-period",
		"insecureAPIConnection":        "insecure-api-connection",
		"tlsCertPath":                  "tls-cert-path",
		"tlsKeyPath":                   "tls-key-path",
		"cdnBaseURL":                   "cdn-base-url",
		"dumpRequests":                 "dump-requests",
	} {
		alias := *cmd.Flags().Lookup(name)
		alias.Name = oldName
		cmd.Flags().AddFlag(&alias)
		must(cmd.Flags().MarkDeprecated(oldName, "use --"+name+" instead"))
	}

	return cmd
}

func getPromptCacheSalt(cmd *cobra.Command) (string, error) {
	if legacyPromptCacheSalt != "" && !sharedPromptCache {
		return "", errors.New("promptCacheSalt requires sharedPromptCache")
	}
	if disablePromptCache && cmd.Flags().Changed("prompt-cache-salt") {
		return "", errors.New("--prompt-cache-salt requires --disable-prompt-cache=false")
	}
	if disablePromptCache && !sharedPromptCache {
		return "", nil
	}
	salt := promptCacheSalt
	switch {
	case sharedPromptCache && legacyPromptCacheSalt == "":
		return openai.RandomPromptCacheSalt(), nil
	case sharedPromptCache:
		// Legacy salts are literals or files, never API-key derivation.
		salt = legacyPromptCacheSalt
	case salt == "api-key":
		return "", nil // Hidden source: derive from the authentication key of each request.
	case salt == "random":
		return openai.RandomPromptCacheSalt(), nil
	}
	if path, ok := strings.CutPrefix(salt, "@"); ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to read prompt cache salt file %q: %w", path, err)
		}
		salt = strings.TrimSpace(string(data))
	}

	if len(salt) < 32 {
		return "", errors.New("prompt cache salt must be at least 32 bytes long")
	}
	// Match vLLM v0.30.0's validate_cache_salt restrictions for the new source flag.
	// Keep the legacy flags' validation unchanged.
	if !sharedPromptCache && (!utf8.ValidString(salt) || utf8.RuneCountInString(salt) > 128 || strings.ContainsAny(salt, "@/\\\x00")) {
		return "", errors.New("prompt cache salt must be valid UTF-8, at most 128 characters, and must not contain '@', '/', '\\', or NUL")
	}

	return salt, nil
}

func getAPIKeyFallback() (server.APIKeyFallback, error) {
	fallback := server.APIKeyFallback(apiKeyFallback)
	switch fallback {
	case server.APIKeyFallbackNever, server.APIKeyFallbackAlways, server.APIKeyFallbackIfPlaceholder:
		return fallback, nil
	default:
		return "", fmt.Errorf("invalid --api-key-fallback value %q: must be never, always, or if-placeholder", apiKeyFallback)
	}
}

func runProxy(cmd *cobra.Command, _ []string) error {
	var log *slog.Logger
	if logFormat == logging.FormatFlagValueText {
		log = logging.NewCLILogger(logLevel, cmd.OutOrStderr())
	} else {
		log = logging.NewLogger(logLevel)
	}

	log.InfoContext(cmd.Context(), "Privatemode encryption proxy", "version", constants.Version())

	fallback, err := getAPIKeyFallback()
	if err != nil {
		return err
	}
	if !cmd.Flags().Changed("api-key-fallback") {
		log.WarnContext(cmd.Context(), "API key fallback defaults to always. The default will change to never in a future release. Set --api-key-fallback explicitly to choose the desired behavior.")
	}

	if (tlsCertPath == "") != (tlsKeyPath == "") {
		return errors.New("TLS certificate and key must be provided together")
	}

	cacheSalt, err := getPromptCacheSalt(cmd)
	if err != nil {
		return fmt.Errorf("getting prompt cache salt: %w", err)
	}

	var apiKey *string
	if cmd.Flags().Changed("api-key") || cmd.Flags().Changed("apiKey") {
		// Trim '@' and read file contents
		if path, ok := strings.CutPrefix(apiKeyStr, "@"); ok {
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
		log.WarnContext(cmd.Context(), "No API key provided. The proxy will not authenticate with the API.")
	}

	if !nvidiaOCSPAllowUnknown && (nvidiaOCSPRevokedGracePeriod > 0) {
		return errors.New("unknown OCSP statuses are disallowed, but revoked statuses are allowed. This is likely to be an erroneous configuration")
	}

	// The value is sent as a header on every forwarded request. Reject anything net/http would
	// refuse at send time, where the failure would show up per request instead of at startup.
	if clientID != "" && !clientIDPattern.MatchString(clientID) {
		return fmt.Errorf("client-id %q must match %s", clientID, clientIDPattern)
	}

	var modelsDocument *server.ModelsDocument
	if modelsDocumentPath != "" {
		modelsDocument, err = server.NewModelsDocument(modelsDocumentPath, log)
		if err != nil {
			return fmt.Errorf("loading models document: %w", err)
		}
	}

	log.InfoContext(cmd.Context(), "Starting proxy")
	flags := setup.Flags{
		Workspace:    workspace,
		ManifestPath: manifestPath,
		ContrastFlags: setup.ContrastFlags{
			CDNBaseURL: cdnBaseURL,
		},
		InsecureAPIConnection:        insecureAPIConnection,
		APIEndpoint:                  apiEndpoint,
		APIKey:                       apiKey,
		APIKeyFallback:               fallback,
		CORSAllowedOrigins:           corsAllowedOrigins,
		PromptCacheSalt:              cacheSalt,
		PromptCacheSaltFromAPIKey:    cmd.Flags().Changed("prompt-cache-salt") && promptCacheSalt == "api-key",
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
		ClientID:       clientID,
		ModelsDocument: modelsDocument,
	}
	manager, _, err := setup.SecretManager(cmd.Context(), flags, log)
	if err != nil {
		return fmt.Errorf("setting up secret manager configuration: %w", err)
	}
	const isApp = false

	listenEndpoint := net.JoinHostPort(listenAddress, port)
	lis, err := net.Listen("tcp", listenEndpoint)
	if err != nil {
		return fmt.Errorf("listening on %q: %w", listenEndpoint, err)
	}
	tlsConfig, err := getTLSConfig(tlsCertPath, tlsKeyPath)
	if err != nil {
		return fmt.Errorf("loading TLS config: %w", err)
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		manager.Run(cmd.Context(), log.With("component", "secret-loop"))
	})

	wg.Go(func() {
		err = setup.
			NewServer(flags, isApp, manager, log).
			Serve(cmd.Context(), lis, tlsConfig)
	})

	wg.Wait()
	return err
}

// getTLSConfig returns the TLS configuration for production.
func getTLSConfig(tlsCertPath, tlsKeyPath string) (*tls.Config, error) {
	if tlsCertPath == "" && tlsKeyPath == "" {
		return nil, nil
	}
	return tlsconfig.New(tlsCertPath, tlsKeyPath)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
