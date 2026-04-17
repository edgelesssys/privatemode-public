// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package server implements the HTTP server to forward encrypted requests to the API.
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
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
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/middleware"
	"github.com/edgelesssys/continuum/internal/oss/mutators"
	"github.com/edgelesssys/continuum/internal/oss/ocspheader"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/persist"
	"github.com/edgelesssys/continuum/internal/oss/process"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
	"github.com/google/uuid"
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
	mux.HandleFunc(openai.ChatCompletionsEndpoint, s.chatRequestHandler(openai.PlainCompletionsRequestFields, openai.PlainCompletionsResponseFields))
	mux.HandleFunc(openai.LegacyCompletionsEndpoint, s.chatRequestHandler(openai.PlainCompletionsRequestFields, openai.PlainCompletionsResponseFields))
	mux.HandleFunc("/unstructured/", s.unstructuredHandler)
	mux.HandleFunc(openai.ModelsEndpoint, s.noEncryptionHandler)
	mux.HandleFunc(openai.EmbeddingsEndpoint, s.embeddingsHandler)
	mux.HandleFunc(openai.TranscriptionsEndpoint, s.transcriptionsHandler)
	mux.HandleFunc(anthropic.MessagesEndpoint, s.chatRequestHandler(anthropic.PlainMessagesRequestFields, anthropic.PlainMessagesResponseFields))

	// Apply middlewares below, handler holds the chain entrypoint
	var handler http.Handler = mux

	handler = passAuthToSecretManagerMiddleware(handler, s.sm)

	// Only apply dumping middleware when a dump directory is configured.
	if strings.TrimSpace(s.dumpRequestsDir) != "" {
		handler = middleware.DumpRequestAndResponse(handler, s.log, s.dumpRequestsDir)
	}

	return handler
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

func (s *Server) inferenceHandler(
	requestMutator func(*RenewableRequestCipher) forwarder.RequestMutator,
	responseMapper func(*RenewableRequestCipher) forwarder.ResponseMapper,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			s.noEncryptionHandler(w, r)
			return
		}

		s.setStaticRequestHeaders(r)

		rc, secret, err := NewRenewableRequestCipher(s.sm, r)
		if err != nil {
			forwarder.HTTPError(w, r, http.StatusInternalServerError, "creating request cipher: %s", err)
			return
		}
		requestID := newRequestID()
		if err := s.setDynamicHeaders(r, *secret, requestID, 0); err != nil {
			forwarder.HTTPError(w, r, http.StatusInternalServerError, "setting dynamic headers: %s", err)
			return
		}

		// Set up retry logic for specific status codes with exponential backoff
		retryCallback := func(statusCode int, errMsg string, attempt int) (bool, time.Duration) {
			switch {
			case attempt <= 1 && (statusCode == 500 && strings.Contains(errMsg, constants.ErrorNoSecretForID)):
				return s.noSecretForIDCallback(w, r, rc, requestID, attempt)
			case attempt <= 1 && strings.Contains(errMsg, "read: connection reset by peer"):
				return s.connectionResetCallback(w, r, rc, requestID, attempt)
			default:
				return false, 0
			}
		}

		s.forwarder.Forward(
			w, r,
			requestMutator(rc),
			withAllowDesktopApp(responseMapper(rc), r),
			forwarder.WithRetryCallback(retryCallback),
		)
	}
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

func (s *Server) chatRequestHandler(
	plainReqFields, plainRespFields forwarder.FieldSelector,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.inferenceHandler(
			func(cw *RenewableRequestCipher) forwarder.RequestMutator {
				return forwarder.RequestMutatorChain(
					mutators.ShardKeyInjector(s.defaultCacheSalt, s.log), // we don't want a shard key for random cache salts, so we inject before
					openai.CacheSaltInjector(func() string {
						if s.defaultCacheSalt == "" {
							return openai.RandomPromptCacheSalt()
						}
						return s.defaultCacheSalt
					}, s.log),
					mutators.ModelHeaderInjector(modelFromRequest),
					forwarder.WithJSONRequestMutation(cw.Encrypt, plainReqFields, s.log),
				)
			},
			func(cw *RenewableRequestCipher) forwarder.ResponseMapper {
				return forwarder.JSONResponseMapper(cw.DecryptResponse, plainRespFields)
			},
		)(w, r)
	}
}

func (s *Server) embeddingsHandler(w http.ResponseWriter, r *http.Request) {
	s.inferenceHandler(
		func(cw *RenewableRequestCipher) forwarder.RequestMutator {
			return forwarder.RequestMutatorChain(
				mutators.ModelHeaderInjector(modelFromRequest),
				forwarder.WithJSONRequestMutation(cw.Encrypt, openai.PlainEmbeddingsRequestFields, s.log),
			)
		},
		func(cw *RenewableRequestCipher) forwarder.ResponseMapper {
			return forwarder.JSONResponseMapper(cw.DecryptResponse, openai.PlainEmbeddingsResponseFields)
		},
	)(w, r)
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

	s.inferenceHandler(
		func(cw *RenewableRequestCipher) forwarder.RequestMutator {
			return forwarder.RequestMutatorChain(
				mutators.ModelHeaderInjector(modelExtractor),
				forwarder.WithFormRequestMutation(cw.Encrypt, openai.PlainTranscriptionRequestFields, s.log),
			)
		},
		func(cw *RenewableRequestCipher) forwarder.ResponseMapper {
			return forwarder.JSONResponseMapper(cw.DecryptResponse, openai.PlainTranscriptionResponseFields)
		},
	)(w, r)
}

func (s *Server) unstructuredHandler(w http.ResponseWriter, r *http.Request) {
	s.inferenceHandler(
		func(cw *RenewableRequestCipher) forwarder.RequestMutator {
			return forwarder.WithRawRequestMutation(cw.Encrypt, s.log)
		},
		func(cw *RenewableRequestCipher) forwarder.ResponseMapper {
			// currently only json response mutation is supported
			return forwarder.JSONResponseMapper(cw.DecryptResponse, nil)
		},
	)(w, r)
}

func (s *Server) noEncryptionHandler(w http.ResponseWriter, r *http.Request) {
	s.setStaticRequestHeaders(r)
	r.Header.Set(constants.RequestIDHeader, newRequestID())

	s.forwarder.Forward(
		w, r,
		forwarder.NoRequestMutation,
		withAllowDesktopApp(forwarder.PassthroughResponseMapper, r),
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

// withAllowDesktopApp wraps a base mapper and adds CORS headers for desktop app origins (app://).
// Handling CORS headers inside the mapper is required because upstream CORS headers are applied
// after the middlewares and overwrite the values we want. Ideally, only relevant upstream headers
// would be copied by the mapper, and CORS would be applied via a middleware.
func withAllowDesktopApp(mapper forwarder.ResponseMapper, req *http.Request) forwarder.ResponseMapper {
	return func(ur *http.Response) (forwarder.Response, error) {
		dr, err := mapper(ur)
		if err != nil {
			return nil, err
		}

		origin := req.Header.Get("Origin")
		if strings.HasPrefix(origin, "app://") {
			dr.GetHeader().Set("Access-Control-Allow-Origin", origin)
		}

		return dr, nil
	}
}

// setDynamicHeaders sets the dynamic headers for the request.
func (s *Server) setDynamicHeaders(r *http.Request, secret secretmanager.Secret, requestID string, attempt int) error {
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
	r.Header.Set(constants.RequestIDHeader, fmt.Sprintf("%s_%d", requestID, attempt))
	return nil
}

func (s *Server) connectionResetCallback(
	w http.ResponseWriter, r *http.Request, rc *RenewableRequestCipher, requestID string, attempt int,
) (bool, time.Duration) {
	// Force a new rc for the next attempt, but keep the same secret
	secret, err := rc.init(r)
	if err != nil {
		s.log.Error("Resetting request cipher", "error", err)
		return false, 0
	}

	if err := s.setDynamicHeaders(r, *secret, requestID, attempt); err != nil {
		forwarder.HTTPError(w, r, http.StatusInternalServerError, "setting dynamic headers: %s", err)
		return false, 0
	}

	// Wait a short duration before retrying
	return true, 50 * time.Millisecond
}

func (s *Server) noSecretForIDCallback(
	w http.ResponseWriter, r *http.Request, rc *RenewableRequestCipher, requestID string, attempt int,
) (bool, time.Duration) {
	// Force a new rc for the next attempt
	secret, err := rc.ResetSecret(r)
	if err != nil {
		s.log.Error("Resetting request cipher", "error", err)
		return false, 0
	}

	if err := s.setDynamicHeaders(r, *secret, requestID, attempt); err != nil {
		forwarder.HTTPError(w, r, http.StatusInternalServerError, "setting dynamic headers: %s", err)
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

func newRequestID() string {
	return "proxy_" + uuid.New().String()
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
