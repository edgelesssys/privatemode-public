// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package server implements the HTTP server to forward encrypted requests to the API.
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/anthropic"
	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/encbody"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/middleware"
	"github.com/edgelesssys/continuum/internal/oss/mutators"
	"github.com/edgelesssys/continuum/internal/oss/ocspheader"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/persist"
	"github.com/edgelesssys/continuum/internal/oss/process"
	"github.com/edgelesssys/continuum/internal/oss/requestid"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
)

// Server implements the HTTP server for the API gateway.
type Server struct {
	apiKey                       *string
	defaultCacheSalt             string // if no salt is set, a random salt will be used
	forwarder                    apiForwarder
	sm                           secretManager
	log                          *slog.Logger
	isApp                        bool
	nvidiaOCSPAllowUnknown       bool
	nvidiaOCSPRevokedGracePeriod time.Duration
	dumpRequestsDir              string
}

// Opts are the options for creating a new [Server].
type Opts struct {
	APIEndpoint                  string
	APIKey                       *string
	ProtocolScheme               forwarder.ProtocolScheme
	PromptCacheSalt              string
	IsApp                        bool
	NvidiaOCSPAllowUnknown       bool
	NvidiaOCSPRevokedGracePeriod time.Duration
	DumpRequestsDir              string
}

type apiForwarder interface {
	Forward(
		w http.ResponseWriter, req *http.Request,
		requestMutator forwarder.RequestMutator, responseMapper forwarder.ResponseMapper,
		opts ...forwarder.Opts,
	)
}

type secretManager interface {
	LatestSecret(ctx context.Context) (secretmanager.Secret, error)
	ForceUpdate(ctx context.Context) error
	OfferAPIKey(context.Context, string) error
}

// New sets up a new Server.
func New(client *http.Client, sm secretManager, opts Opts, log *slog.Logger) *Server {
	log.Info("Version", slog.String("version", constants.Version()))
	fwd := forwarder.New(client, opts.APIEndpoint, opts.ProtocolScheme, log)

	return &Server{
		apiKey:                       opts.APIKey,
		defaultCacheSalt:             opts.PromptCacheSalt,
		forwarder:                    fwd,
		sm:                           sm,
		log:                          log,
		isApp:                        opts.IsApp,
		nvidiaOCSPAllowUnknown:       opts.NvidiaOCSPAllowUnknown,
		nvidiaOCSPRevokedGracePeriod: opts.NvidiaOCSPRevokedGracePeriod,
		dumpRequestsDir:              opts.DumpRequestsDir,
	}
}

// Serve starts the server on the given port.
// If tlsConfig is nil, the server will start without TLS.
func (s *Server) Serve(ctx context.Context, lis net.Listener, tlsConfig *tls.Config) error {
	server := &http.Server{
		Addr:      lis.Addr().String(),
		Handler:   s.GetHandler(),
		TLSConfig: tlsConfig,
		ErrorLog:  slog.NewLogLogger(s.log.Handler(), slog.LevelError),
	}
	return process.HTTPServeContext(ctx, server, lis, s.log)
}

// GetHandler returns an HTTP handler that routes requests to the appropriate handler.
func (s *Server) GetHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(openai.ChatCompletionsEndpoint, s.chatRequestHandler)
	mux.HandleFunc(openai.LegacyCompletionsEndpoint, s.chatRequestHandler)
	mux.HandleFunc("/unstructured/", s.unstructuredHandler)
	mux.HandleFunc(openai.ModelsEndpoint, s.forwardUnencrypted)
	mux.HandleFunc(openai.EmbeddingsEndpoint, s.embeddingsHandler)
	mux.HandleFunc(openai.TranscriptionsEndpoint, s.transcriptionsHandler)
	mux.HandleFunc(anthropic.MessagesEndpoint, s.chatRequestHandler)
	mux.HandleFunc("/readyz", s.readinessHandler)

	// Apply middlewares below, handler holds the chain entrypoint
	var handler http.Handler = mux

	handler = passAuthToSecretManagerMiddleware(handler, s.sm)

	// Only apply dumping middleware when a dump directory is configured.
	if strings.TrimSpace(s.dumpRequestsDir) != "" {
		handler = middleware.DumpRequestAndResponse(handler, s.log, s.dumpRequestsDir)
	}

	handler = middleware.LogRequestSummary(handler, s.log, true)

	handler = addRequestID(handler)

	return handler
}

func (s *Server) readinessHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// addRequestID generates a fresh request ID and stores it in the request context.
func addRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := requestid.New()
		ctx := requestid.WithContext(r.Context(), requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// passAuthToSecretManagerMiddleware extracts the bearer token from the request and passes it to
// the secret manager.
func passAuthToSecretManagerMiddleware(next http.Handler, sm secretManager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiKey, err := auth.GetAuth(auth.Bearer, r.Header); err == nil {
			if err := sm.OfferAPIKey(r.Context(), apiKey); err != nil {
				forwarder.HTTPError(w, r, http.StatusUnauthorized, "trying API key: %s", err)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) forwardEncrypted(
	w http.ResponseWriter, r *http.Request,
	requestMutator forwarder.RequestMutator,
) {
	var ce *encbody.ClientExchange
	ctx := r.Context()
	// request ID is always stored in context by middleware
	requestID, _ := requestid.FromContext(ctx)
	attempt := 0

	// Set up retry logic for specific status codes
	retryCallback := func(statusCode int, errMsg string, callbackAttempt int) (bool, time.Duration) {
		attempt = callbackAttempt

		if callbackAttempt > 1 {
			return false, 0
		}

		switch {
		case (statusCode == 400 || statusCode == 500) && strings.Contains(errMsg, constants.ErrorNoSecretForID):
			return s.noSecretForIDCallback(ctx)
		case strings.Contains(errMsg, "read: connection reset by peer"):
			// The encrypted exchange is rebuilt per attempt in fullRequestMutator, so
			// just back off briefly and retry with the same secret.
			return true, 50 * time.Millisecond
		default:
			return false, 0
		}
	}

	fullRequestMutator := func(req *http.Request) error {
		secret, err := s.sm.LatestSecret(req.Context())
		if err != nil {
			return fmt.Errorf("getting latest secret: %w", err)
		}

		ce, err = encbody.InitializeClientExchange(secret.Data)
		if err != nil {
			return fmt.Errorf("initializing encrypted exchange: %w", err)
		}

		if err := requestMutator(req); err != nil {
			return fmt.Errorf("applying endpoint-specific mutator: %w", err)
		}

		if err := ce.EncryptRequest(req); err != nil {
			return fmt.Errorf("encrypting request: %w", err)
		}

		s.setStaticRequestHeaders(req)

		if err := s.setEncryptionHeaders(req, secret, requestID, attempt); err != nil {
			return fmt.Errorf("setting headers on upstream request: %w", err)
		}

		return nil
	}

	fullResponseMapper := func(resp *http.Response) (forwarder.Response, error) {
		logging.AddSummaryAttrs(ctx, "backendRequestID", requestid.FromHeader(resp.Header))

		dsResp, err := ce.DecryptResponse(resp, constants.MaxEncryptedFrameSize)
		if err != nil {
			if errors.Is(err, encbody.ErrNotEncrypted) && resp.StatusCode >= 400 {
				// Error responses originating outside the TCB are unencrypted
				return forwarder.PassthroughResponseMapper(resp)
			}
			return nil, fmt.Errorf("decrypting response: %w", err)
		}

		return dsResp, nil
	}

	s.forwarder.Forward(
		w, r,
		fullRequestMutator,
		fullResponseMapper,
		forwarder.WithRetryCallback(retryCallback),
	)
}

func modelFromRequest(req *http.Request) (string, error) {
	type modelRequest struct {
		Model string `json:"model"`
	}

	msg, err := unmarshalJSONBody[modelRequest](req)
	if err != nil {
		return "", fmt.Errorf("parsing request: %w", err)
	}
	if msg.Model == "" {
		return "", fmt.Errorf("no model specified in request")
	}

	return msg.Model, nil
}

func (s *Server) chatRequestHandler(w http.ResponseWriter, r *http.Request) {
	s.forwardEncrypted(
		w, r,
		forwarder.RequestMutatorChain(
			mutators.ShardKeyInjector(s.defaultCacheSalt), // we don't want a shard key for random cache salts, so we inject before
			openai.CacheSaltInjector(func() string {
				if s.defaultCacheSalt == "" {
					return openai.RandomPromptCacheSalt()
				}
				return s.defaultCacheSalt
			}, s.log),
			mutators.ModelHeaderInjector(modelFromRequest),
		),
	)
}

func (s *Server) embeddingsHandler(w http.ResponseWriter, r *http.Request) {
	s.forwardEncrypted(
		w, r,
		mutators.ModelHeaderInjector(modelFromRequest),
	)
}

func (s *Server) transcriptionsHandler(w http.ResponseWriter, r *http.Request) {
	modelExtractor := func(req *http.Request) (string, error) {
		clonedReq, err := persist.CloneRequestUnlimited(req)
		if err != nil {
			return "", fmt.Errorf("reading request: %w", err)
		}

		if err := clonedReq.ParseMultipartForm(constants.MaxFileSizeBytes); err != nil {
			return "", fmt.Errorf("parsing multipart form: %w", err)
		}
		defer func() { _ = clonedReq.MultipartForm.RemoveAll() }()

		modelName := clonedReq.PostFormValue("model")
		if len(modelName) == 0 {
			return "", fmt.Errorf("no model specified in request")
		}

		return modelName, nil
	}

	s.forwardEncrypted(
		w, r,
		mutators.ModelHeaderInjector(modelExtractor),
	)
}

func (s *Server) unstructuredHandler(w http.ResponseWriter, r *http.Request) {
	s.forwardEncrypted(
		w, r,
		forwarder.NoRequestMutation,
	)
}

func (s *Server) forwardUnencrypted(w http.ResponseWriter, r *http.Request) {
	s.setStaticRequestHeaders(r)
	// request ID is always stored in context by middleware
	requestID, _ := requestid.FromContext(r.Context())
	r.Header.Set(requestid.UserHeader, "proxy_"+requestID)

	s.forwarder.Forward(
		w, r,
		forwarder.NoRequestMutation,
		forwarder.PassthroughResponseMapper,
	)
}

func (s *Server) getClientHeader() string {
	if s.isApp {
		return constants.PrivatemodeClientApp
	}

	return constants.PrivatemodeClientProxy
}

// setStaticRequestHeaders sets static headers for the request. These are the header values
// that are guaranteed to be immutable over a request's lifetime.
func (s *Server) setStaticRequestHeaders(r *http.Request) {
	if s.apiKey != nil {
		r.Header.Set("Authorization", fmt.Sprintf("%s %s", auth.Bearer, *s.apiKey))
	}
	r.Header.Set(constants.PrivatemodeVersionHeader, constants.Version())
	r.Header.Set(constants.PrivatemodeOSHeader, runtime.GOOS)
	r.Header.Set(constants.PrivatemodeArchitectureHeader, runtime.GOARCH)
	r.Header.Set(constants.PrivatemodeClientHeader, s.getClientHeader())
}

// setEncryptionHeaders sets the encryption-related headers for the request.
func (s *Server) setEncryptionHeaders(r *http.Request, secret secretmanager.Secret, requestID string, attempt int) error {
	ocspAllowedStatuses := []ocspheader.AllowStatus{ocspheader.AllowStatusGood}
	if s.nvidiaOCSPRevokedGracePeriod > 0 {
		// In theory, we could always add the `revoked` status, since it will render
		// ineffective if the grace period is 0, but it might look strange to the user
		// to find a `revoked` status in the policy header, so we only add it if the
		// grace period is set.
		ocspAllowedStatuses = append(ocspAllowedStatuses, ocspheader.AllowStatusRevoked)
	}
	if s.nvidiaOCSPAllowUnknown {
		ocspAllowedStatuses = append(ocspAllowedStatuses, ocspheader.AllowStatusUnknown)
	}

	if len(secret.Data) < 32 {
		return fmt.Errorf("secret data too short: got %d bytes, need at least 32", len(secret.Data))
	}
	ocspPolicyHeader, ocspMACHeader, err := getOcspHeaders(
		ocspAllowedStatuses, time.Now().Add(-s.nvidiaOCSPRevokedGracePeriod),
		[32]byte(secret.Data[:32]),
	)
	if err != nil {
		return fmt.Errorf("generating OCSP headers: %w", err)
	}

	r.Header.Set(constants.PrivatemodeNvidiaOCSPPolicyHeader, ocspPolicyHeader)
	r.Header.Set(constants.PrivatemodeNvidiaOCSPPolicyMACHeader, ocspMACHeader)
	r.Header.Set(constants.PrivatemodeSecretIDHeader, secret.ID)
	r.Header.Set(requestid.UserHeader, fmt.Sprintf("proxy_%s_%d", requestID, attempt))
	return nil
}

func (s *Server) noSecretForIDCallback(ctx context.Context) (bool, time.Duration) {
	// Force a fresh secret for the next attempt; the mutator picks it up via LatestSecret.
	if err := s.sm.ForceUpdate(ctx); err != nil {
		s.log.ErrorContext(ctx, "Forcing secret update", "error", err)
		return false, 0
	}

	// Retry immediately
	return true, 0
}

// getOcspHeaders generates the OCSP headers based on the allowed statuses and revocation time.
// It returns the policy header and the MAC header.
func getOcspHeaders(allowedStatuses []ocspheader.AllowStatus, revocNbf time.Time, secret [32]byte) (
	ocspPolicyHeader string, ocspMACHeader string, err error,
) {
	header := ocspheader.NewHeader(allowedStatuses, revocNbf)
	policyHeader, err := header.Marshal()
	if err != nil {
		return "", "", fmt.Errorf("marshaling OCSP header: %w", err)
	}

	policyMACHeader, err := header.MarshalMACHeader(secret)
	if err != nil {
		return "", "", fmt.Errorf("marshaling OCSP MAC header: %w", err)
	}

	return policyHeader, policyMACHeader, nil
}

// unmarshalJSONBody uses [persist.ReadBodyUnlimited] to read r's body and then unmarshals it.
func unmarshalJSONBody[T any](r *http.Request) (T, error) {
	var v T

	body, err := persist.ReadBodyUnlimited(r)
	if err != nil {
		return v, fmt.Errorf("reading body: %w", err)
	}

	if err = json.Unmarshal(body, &v); err != nil {
		return v, fmt.Errorf("decoding JSON: %w", err)
	}

	return v, nil
}
