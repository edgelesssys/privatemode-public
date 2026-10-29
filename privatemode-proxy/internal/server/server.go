// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package server implements the HTTP server to forward encrypted requests to the API.
package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"

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
	"github.com/tidwall/gjson"
)

// APIKeyFallback controls when the API key configured via the CLI flag replaces request authentication.
type APIKeyFallback string

const (
	// APIKeyFallbackAlways uses the configured API key for every request.
	APIKeyFallbackAlways APIKeyFallback = "always"
	// APIKeyFallbackNever never puts the CLI-provided API key into requests.
	APIKeyFallbackNever APIKeyFallback = "never"
	// APIKeyFallbackIfPlaceholder replaces only the literal Bearer placeholder header.
	APIKeyFallbackIfPlaceholder APIKeyFallback = "if-placeholder"
)

// Server implements the HTTP server for the API gateway.
type Server struct {
	apiKey                       *string
	apiKeyFallback               APIKeyFallback
	timeoutRead                  time.Duration
	timeoutReadHeader            time.Duration
	timeoutWrite                 time.Duration
	timeoutIdle                  time.Duration
	defaultCacheSalt             string // if no salt is set, a random salt will be used
	promptCacheSaltFromAPIKey    bool
	forwarder                    apiForwarder
	sm                           secretManager
	log                          *slog.Logger
	nvidiaOCSPAllowUnknown       bool
	nvidiaOCSPRevokedGracePeriod time.Duration
	dumpRequestsDir              string
	corsAllowedOrigins           []string
	clientID                     string
	modelsDocument               *ModelsDocument
}

// Opts are the options for creating a new [Server].
type Opts struct {
	APIEndpoint                  string
	APIKey                       *string
	APIKeyFallback               APIKeyFallback
	ProtocolScheme               forwarder.ProtocolScheme
	TimeoutRead                  time.Duration
	TimeoutReadHeader            time.Duration
	TimeoutWrite                 time.Duration
	TimeoutIdle                  time.Duration
	PromptCacheSalt              string
	PromptCacheSaltFromAPIKey    bool
	NvidiaOCSPAllowUnknown       bool
	NvidiaOCSPRevokedGracePeriod time.Duration
	DumpRequestsDir              string
	CORSAllowedOrigins           []string
	ClientID                     string
	// ModelsDocument, if set, is served at the models endpoint instead of forwarding the request.
	ModelsDocument *ModelsDocument
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
	OfferCredential(context.Context, auth.Credential) error
}

// New sets up a new Server.
func New(client *http.Client, sm secretManager, opts Opts, log *slog.Logger) *Server {
	fwd := forwarder.New(client, opts.APIEndpoint, opts.ProtocolScheme, forwarder.UpstreamAPI, log)
	if opts.APIKeyFallback == "" {
		opts.APIKeyFallback = APIKeyFallbackAlways
	}

	return &Server{
		apiKey:                       opts.APIKey,
		apiKeyFallback:               opts.APIKeyFallback,
		timeoutRead:                  opts.TimeoutRead,
		timeoutReadHeader:            opts.TimeoutReadHeader,
		timeoutWrite:                 opts.TimeoutWrite,
		timeoutIdle:                  opts.TimeoutIdle,
		defaultCacheSalt:             opts.PromptCacheSalt,
		promptCacheSaltFromAPIKey:    opts.PromptCacheSaltFromAPIKey,
		forwarder:                    fwd,
		sm:                           sm,
		log:                          log,
		nvidiaOCSPAllowUnknown:       opts.NvidiaOCSPAllowUnknown,
		nvidiaOCSPRevokedGracePeriod: opts.NvidiaOCSPRevokedGracePeriod,
		dumpRequestsDir:              opts.DumpRequestsDir,
		corsAllowedOrigins:           opts.CORSAllowedOrigins,
		clientID:                     opts.ClientID,
		modelsDocument:               opts.ModelsDocument,
	}
}

// Serve starts the server on the given port.
// If tlsConfig is nil, the server will start without TLS.
func (s *Server) Serve(ctx context.Context, lis net.Listener, tlsConfig *tls.Config) error {
	server := &http.Server{
		Addr:              lis.Addr().String(),
		Handler:           s.GetHandler(),
		TLSConfig:         tlsConfig,
		ErrorLog:          logging.NewHTTPErrorLogger(s.log),
		ReadTimeout:       s.timeoutRead,
		ReadHeaderTimeout: s.timeoutReadHeader,
		WriteTimeout:      s.timeoutWrite,
		IdleTimeout:       s.timeoutIdle,
	}
	return process.HTTPServeContext(ctx, server, lis, s.log)
}

// GetHandler returns an HTTP handler that routes requests to the appropriate handler.
func (s *Server) GetHandler() http.Handler {
	// API routes take credentials. Unmatched routes (404/405) and readiness don't.
	api := func(h http.HandlerFunc) http.Handler {
		handler := passAuthToSecretManagerMiddleware(h, s.sm)
		if strings.TrimSpace(s.dumpRequestsDir) != "" {
			handler = middleware.DumpRequestAndResponse(handler, s.log, s.dumpRequestsDir)
		}
		// Bound body reads before dumping, secret management, or forwarding.
		return limitRequestBody(handler)
	}
	mux := http.NewServeMux()
	mux.Handle("POST "+constants.OpenAIChatCompletionsEndpoint, api(s.chatRequestHandler))
	mux.Handle("POST "+constants.OpenAILegacyCompletionsEndpoint, api(s.chatRequestHandler))
	mux.Handle("POST "+constants.UnstructuredEndpoint+"/", api(s.unstructuredHandler))
	if s.modelsDocument != nil {
		mux.Handle("GET "+constants.OpenAIModelsEndpoint, api(s.modelsDocument.ListModels))
		mux.Handle("GET "+constants.OpenAIModelsEndpoint+"/{model}", api(s.modelsDocument.RetrieveModel))
	} else {
		mux.Handle("GET "+constants.OpenAIModelsEndpoint, api(s.forwardUnencrypted))
		mux.Handle("GET "+constants.OpenAIModelsEndpoint+"/{model}", api(s.forwardUnencrypted))
	}
	mux.Handle("POST "+constants.OpenAIEmbeddingsEndpoint, api(s.embeddingsHandler))
	mux.Handle("POST "+constants.OpenAITranscriptionsEndpoint, api(s.transcriptionsHandler))
	mux.Handle("POST "+constants.AnthropicMessagesEndpoint, api(s.chatRequestHandler))
	mux.HandleFunc("GET /readyz", s.readinessHandler)

	// Apply middlewares below, handler holds the chain entrypoint
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
		logging.AddSummaryAttrs(r.Context(), "httpRoute", r.Pattern) // empty for 404/405
	})

	handler = cors(handler, mux, s.corsAllowedOrigins)

	handler = middleware.LogRequestSummary(handler, s.log, true)

	handler = addRequestID(handler)

	return handler
}

func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		maxBytes := int64(constants.MaxFileSizeBytes)
		msg := constants.MaxBodySizeExceededMsg
		if strings.HasPrefix(r.URL.Path, constants.UnstructuredEndpoint+"/") {
			maxBytes = constants.MaxUnstructuredBodySize
			msg = constants.MaxUnstructuredBodySizeExceededMsg
		}
		if r.ContentLength > maxBytes {
			forwarder.HTTPError(w, r, http.StatusRequestEntityTooLarge, "%s", msg)
			return
		}
		if _, err := persist.ReadBody(w, r, maxBytes); err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				forwarder.HTTPError(w, r, http.StatusRequestEntityTooLarge, "%s", msg)
			} else {
				forwarder.HTTPError(w, r, http.StatusBadRequest, "failed to read request body")
			}
			return
		}
		next.ServeHTTP(w, r)
	})
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
			if err := sm.OfferCredential(r.Context(), auth.APIKey(apiKey)); err != nil {
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

		s.setStaticRequestHeaders(req)
		if err := requestMutator(req); err != nil {
			return fmt.Errorf("applying endpoint-specific mutator: %w", err)
		}

		authenticatedHeaders, err := s.ocspPolicyHeaders()
		if err != nil {
			return fmt.Errorf("generating OCSP policy: %w", err)
		}
		if err = ce.EncryptRequest(req, authenticatedHeaders); err != nil {
			return fmt.Errorf("encrypting request: %w", err)
		}

		req.Header.Set(constants.PrivatemodeSecretIDHeader, secret.ID)
		req.Header.Set(requestid.UserHeader, fmt.Sprintf("proxy_%s_%d", requestID, attempt))

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
		return "", forwarder.NewError("parsing request", err, http.StatusBadRequest)
	}
	if msg.Model == "" {
		return "", forwarder.NewError("validating request", errors.New("no model specified in request"), http.StatusBadRequest)
	}

	return msg.Model, nil
}

func (s *Server) chatRequestHandler(w http.ResponseWriter, r *http.Request) {
	s.forwardEncrypted(w, r, func(req *http.Request) error {
		cacheSalt := s.defaultCacheSalt
		if cacheSalt == "" && s.promptCacheSaltFromAPIKey {
			body, err := persist.ReadBodyUnlimited(req)
			if err != nil {
				return err
			}
			if len(body) > 0 && gjson.GetBytes(body, "cache_salt").String() == "" {
				cacheSalt = promptCacheSaltFromAPIKey(req.Header)
			}
		}
		return forwarder.RequestMutatorChain(
			mutators.ModelHeaderInjector(modelFromRequest),
			mutators.ShardKeyInjector(cacheSalt), // inject before generating random salts to avoid sharding on them
			openai.CacheSaltInjector(func() string {
				if cacheSalt == "" {
					return openai.RandomPromptCacheSalt()
				}
				return cacheSalt
			}, s.log),
		)(req)
	})
}

func promptCacheSaltFromAPIKey(header http.Header) string {
	apiKey, err := auth.GetAuth(auth.Bearer, header)
	if err != nil || apiKey == "" {
		// Leave random salt generation to the injector to avoid sharding on random salts.
		return ""
	}
	mac := hmac.New(sha256.New, []byte(apiKey))
	mac.Write([]byte("privatemode/prompt-cache-salt/v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) embeddingsHandler(w http.ResponseWriter, r *http.Request) {
	s.forwardEncrypted(
		w, r,
		forwarder.RequestMutatorChain(
			mutators.ModelHeaderInjector(modelFromRequest),
			mutators.ContentLengthTokensInjector,
		),
	)
}

func (s *Server) transcriptionsHandler(w http.ResponseWriter, r *http.Request) {
	modelExtractor := func(req *http.Request) (string, error) {
		clonedReq, err := persist.CloneRequestUnlimited(req)
		if err != nil {
			return "", fmt.Errorf("reading request: %w", err)
		}

		if err := clonedReq.ParseMultipartForm(constants.MaxFileSizeBytes); err != nil {
			return "", forwarder.NewError("parsing multipart form", err, http.StatusBadRequest)
		}
		defer func() { _ = clonedReq.MultipartForm.RemoveAll() }()

		modelName := clonedReq.PostFormValue("model")
		if len(modelName) == 0 {
			return "", forwarder.NewError("validating request", errors.New("no model specified in request"), http.StatusBadRequest)
		}

		return modelName, nil
	}

	s.forwardEncrypted(
		w, r,
		forwarder.RequestMutatorChain(
			mutators.ModelHeaderInjector(modelExtractor),
			mutators.ContentLengthTokensInjector,
		),
	)
}

func (s *Server) unstructuredHandler(w http.ResponseWriter, r *http.Request) {
	s.forwardEncrypted(
		w, r,
		mutators.ContentLengthTokensInjector,
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

// setStaticRequestHeaders sets static headers for the request. These are the header values
// that are guaranteed to be immutable over a request's lifetime.
func (s *Server) setStaticRequestHeaders(r *http.Request) {
	if s.apiKey != nil && (s.apiKeyFallback == APIKeyFallbackAlways ||
		(s.apiKeyFallback == APIKeyFallbackIfPlaceholder && r.Header.Get("Authorization") == "Bearer placeholder")) {
		r.Header.Set("Authorization", fmt.Sprintf("%s %s", auth.Bearer, *s.apiKey))
	}
	r.Header.Set(constants.PrivatemodeVersionHeader, constants.Version())
	r.Header.Set(constants.PrivatemodeOSHeader, runtime.GOOS)
	r.Header.Set(constants.PrivatemodeArchitectureHeader, runtime.GOARCH)
	r.Header.Set(constants.PrivatemodeClientHeader, s.clientID)
}

// ocspPolicyHeaders builds the authenticated policy metadata for an inference request.
func (s *Server) ocspPolicyHeaders() (map[string]string, error) {
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

	header := ocspheader.NewHeader(ocspAllowedStatuses, time.Now().Add(-s.nvidiaOCSPRevokedGracePeriod))
	policy, err := header.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshaling OCSP policy: %w", err)
	}
	return map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: policy}, nil
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
