// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// Package server implements the HTTP server to forward encrypted requests to the API.
package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/edgelesssys/continuum/internal/gpl/auth"
	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/internal/gpl/logging"
	"github.com/edgelesssys/continuum/internal/gpl/ocspheader"
	"github.com/edgelesssys/continuum/internal/gpl/openai"
	"github.com/edgelesssys/continuum/internal/gpl/process"
	"github.com/edgelesssys/continuum/internal/gpl/secretmanager"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
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
}

type apiForwarder interface {
	ForwardWithRetry(
		w http.ResponseWriter, req *http.Request,
		requestMutator forwarder.RequestMutator, responseMutator forwarder.ResponseMutator,
		headerMutator forwarder.HeaderMutator, retryCallback forwarder.RetryCallback,
	)
}

// New sets up a new Server.
func New(client *http.Client, sm secretManager, opts Opts, log *slog.Logger) *Server {
	log.Info("version", slog.String("version", constants.Version()))
	fwd := forwarder.NewWithClient(client, opts.APIEndpoint, opts.ProtocolScheme, log)

	return &Server{
		apiKey:                       opts.APIKey,
		defaultCacheSalt:             opts.PromptCacheSalt,
		forwarder:                    fwd,
		sm:                           sm,
		log:                          log,
		isApp:                        opts.IsApp,
		nvidiaOCSPAllowUnknown:       opts.NvidiaOCSPAllowUnknown,
		nvidiaOCSPRevokedGracePeriod: opts.NvidiaOCSPRevokedGracePeriod,
	}
}

// Serve starts the server on the given port.
// If tlsConfig is nil, the server will start without TLS.
func (s *Server) Serve(ctx context.Context, lis net.Listener, tlsConfig *tls.Config) error {
	server := &http.Server{
		Addr:      lis.Addr().String(),
		Handler:   s.GetHandler(),
		TLSConfig: tlsConfig,
		ErrorLog:  logging.NewLogWrapper(s.log),
	}
	return process.HTTPServeContext(ctx, server, lis, s.log)
}

// GetHandler returns an HTTP handler that routes requests to the appropriate handler.
func (s *Server) GetHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(openai.ChatCompletionsEndpoint, s.chatCompletionsHandler)
	mux.HandleFunc(openai.LegacyCompletionsEndpoint, s.chatCompletionsHandler) // Reuse the same handler as for /v1/chat/completions since the unencrypted fields are the same
	mux.HandleFunc("/unstructured/", s.unstructuredHandler)
	mux.HandleFunc(openai.ModelsEndpoint, s.noEncryptionHandler)
	mux.HandleFunc(openai.EmbeddingsEndpoint, s.embeddingsHandler)
	mux.HandleFunc(openai.TranscriptionsEndpoint, s.transcriptionsHandler)
	mux.HandleFunc(openai.TranslationsEndpoint, s.translationsHandler)

	mux.HandleFunc("/", http.NotFound) // Reject requests to unknown endpoints

	return mux
}

func (s *Server) useRandomCacheSalt() bool {
	return s.defaultCacheSalt == ""
}

func (s *Server) cacheSaltInjector() forwarder.RequestMutator {
	var cacheSaltGenerator func() (string, error)
	if s.useRandomCacheSalt() {
		cacheSaltGenerator = openai.RandomPromptCacheSalt
	} else {
		cacheSaltGenerator = func() (string, error) {
			return s.defaultCacheSalt, nil
		}
	}

	return openai.CacheSaltInjector(cacheSaltGenerator, s.log)
}

func (s *Server) generateShardKey(cacheSalt string, content string) (string, error) {
	cacheSaltHash := sha256.Sum256([]byte(cacheSalt))
	shardKeyStr := hex.EncodeToString(cacheSaltHash[:])[:constants.CacheSaltHashLength]

	// Estimate number of tokens n as content length // 4
	n := len(content) / 4

	// Currently, only 1Mio tokens to limit the shard key size. Limiting factors are proxies,
	// where nginx supports only 4kb. But currently, this only goes to the API Gateway such
	// that we could also work with headers larger than 4kb. Envoy also supports more. But
	// could still be a problem for client side proxies.
	//
	// For extending this beyond 1Mio token context size we should have a clear plan on how to
	// support larger keys and/or compress a bit more for large context (e.g., > 100k tokens).
	if n > 1_000_000 {
		s.log.Error("Context too large for shard key generation", slog.Int("tokens", n))
		return "", fmt.Errorf("context too large: ~%d tokens", n)
	}

	blockSize := constants.ShardKeyFirstBoundaryBlocksPerChar * constants.CacheBlockSizeTokens

	// No caching if n < blockSize
	// -> return the base shard key immediately
	if n < blockSize {
		return shardKeyStr, nil
	}

	// Iterate over content, starting with step size 16, doubling with each step
	// using 4 chars to represent 1 token.
	contentBytes := []byte(content)

	// Use the cache salt as initial hash.
	var chunkHash [32]byte
	copy(chunkHash[:], cacheSaltHash[:])
	shardKeyStr += "-"
	for i := 0; i+blockSize <= len(contentBytes)/4; {
		end := i + blockSize
		chunk := contentBytes[i*4 : end*4]

		// We prefix the chunk with the cache salt to avoid exposing any information
		// and to make the sequence unique even if there are minor changes not captured by the
		// 6 bit value extracted below. This also avoids side channel attacks, as the cache
		// salt is never exposed.
		chunkHash = sha256.Sum256(append(chunkHash[:], chunk...))
		last6Bits := chunkHash[len(chunkHash)-1] & 0x3F
		shardKeyStr += string("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"[last6Bits])

		// increase step size
		// - step = 16 from 16...100k -> 62 chars
		// - step = 128 from 1k...100k -> 774 chars
		// - step = 512 from 100k...1M -> 1758 chars
		i += blockSize
		switch i {
		case constants.ShardKeyFirstBoundaryBlocks * constants.CacheBlockSizeTokens:
			blockSize = constants.ShardKeySecondBoundaryBlocksPerChar * constants.CacheBlockSizeTokens
		case constants.ShardKeySecondBoundaryBlocks * constants.CacheBlockSizeTokens:
			blockSize = constants.ShardKeyThirdBoundaryBlocksPerChar * constants.CacheBlockSizeTokens
		}
	}

	return shardKeyStr, nil
}

func (s *Server) shardKeyInjector() forwarder.RequestMutator {
	// Reads the cache salt and generates a shard key using sha256.
	// Returns an error if there is no cache salt in the request body.
	return func(r *http.Request) error {
		bodyBytes, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			return fmt.Errorf("reading request body: %w", err)
		}

		httpBody := string(bodyBytes)
		if len(httpBody) == 0 {
			return nil
		}
		cacheSalt := gjson.Get(httpBody, "cache_salt").String()

		// If there is no explicit cache salt, we use the default cache salt.
		if cacheSalt == "" && !s.useRandomCacheSalt() {
			cacheSalt = s.defaultCacheSalt
		}

		// If there is no cache salt, we use default sharding without a shard key.
		if cacheSalt != "" {
			// /chat/completions
			tools := gjson.Get(httpBody, "tools").String()
			messages := gjson.Get(httpBody, "messages").String()

			// /completions
			prompt := gjson.Get(httpBody, "prompt").String()
			suffix := gjson.Get(httpBody, "suffix").String()

			// NOTE: The order is important and must match the chat template of the model.
			// For many models, tools are defined first, whithin or after the system message.
			// This is the case for Llama and DeepSeek. Gemma does not have tools right now.
			//
			// Mistral puts tools right before the last user message. Once we use a model
			// that does not store tools in the beginning, we may want to create a
			// model-specific shard key to avoid cache misses due to changing tools.
			// Potentially, we may also adjust the chat template for such models but this
			// could have a performance impact.
			content := tools + messages + prompt + suffix
			shardKey, err := s.generateShardKey(cacheSalt, content)
			if err != nil {
				return fmt.Errorf("generating shard key: %w", err)
			}

			// TODO(dr75): For now don't use the full shard key as the load balancing
			// implementation is still missing and using the full key now would destroy
			// cache-awareness.
			r.Header.Set(constants.PrivatemodeShardKeyHeader, shardKey[:constants.CacheSaltHashLength])
		}
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		return nil
	}
}

func (s *Server) inferenceHandler(
	requestMutator func(*RenewableRequestCipher) forwarder.RequestMutator, responseMutator func(*RenewableRequestCipher) forwarder.ResponseMutator,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		s.setStaticRequestHeaders(r)

		rc, secret, err := NewRenewableRequestCipher(s.sm, r)
		if err != nil {
			forwarder.HTTPError(w, r, http.StatusInternalServerError, "creating request cipher: %s", err)
			return
		}
		if err := s.setDynamicHeaders(r, *secret); err != nil {
			forwarder.HTTPError(w, r, http.StatusInternalServerError, "setting dynamic headers: %s", err)
			return
		}

		// Set up retry logic for specific status codes with exponential backoff
		retryCallback := func(statusCode int, body []byte, attempt int) (bool, time.Duration) {
			shouldRetry := statusCode == 500 && attempt <= 1 && strings.Contains(string(body), constants.ErrorNoSecretForID)
			if !shouldRetry {
				return false, 0
			}

			// Force a new rc for the next attempt
			secret, err := rc.ResetSecret(r)
			if err != nil {
				s.log.Error("resetting request cipher", "error", err)
				return false, 0
			}

			if err := s.setDynamicHeaders(r, *secret); err != nil {
				forwarder.HTTPError(w, r, http.StatusInternalServerError, "setting dynamic headers: %s", err)
				return false, 0
			}

			// For now only one retry immediately as we only handle NoSecretForID
			return true, 0
		}

		s.forwarder.ForwardWithRetry(
			w, r,
			requestMutator(rc),
			responseMutator(rc),
			allowWails,
			retryCallback,
		)
	}
}

func (s *Server) chatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	s.inferenceHandler(
		func(cw *RenewableRequestCipher) forwarder.RequestMutator {
			return forwarder.RequestMutatorChain(
				s.shardKeyInjector(), // we don't want a shard key for random cache salts, so we inject before
				s.cacheSaltInjector(),
				forwarder.WithFullJSONRequestMutation(cw.Encrypt, openai.PlainCompletionsRequestFields, s.log),
			)
		},
		func(cw *RenewableRequestCipher) forwarder.ResponseMutator {
			return forwarder.WithFullJSONResponseMutation(cw.DecryptResponse, openai.PlainCompletionsResponseFields, false)
		},
	)(w, r)
}

func (s *Server) embeddingsHandler(w http.ResponseWriter, r *http.Request) {
	s.inferenceHandler(
		func(cw *RenewableRequestCipher) forwarder.RequestMutator {
			return forwarder.WithFullJSONRequestMutation(cw.Encrypt, openai.PlainEmbeddingsRequestFields, s.log)
		},
		func(cw *RenewableRequestCipher) forwarder.ResponseMutator {
			return forwarder.WithFullJSONResponseMutation(cw.DecryptResponse, openai.PlainEmbeddingsResponseFields, false)
		},
	)(w, r)
}

func (s *Server) transcriptionsHandler(w http.ResponseWriter, r *http.Request) {
	s.inferenceHandler(
		func(cw *RenewableRequestCipher) forwarder.RequestMutator {
			return forwarder.WithFormRequestMutation(cw.Encrypt, openai.PlainTranscriptionFields, s.log)
		},
		func(cw *RenewableRequestCipher) forwarder.ResponseMutator {
			return forwarder.WithFullResponseMutation(cw.DecryptResponse)
		},
	)(w, r)
}

func (s *Server) translationsHandler(w http.ResponseWriter, r *http.Request) {
	s.inferenceHandler(
		func(cw *RenewableRequestCipher) forwarder.RequestMutator {
			return forwarder.WithFormRequestMutation(cw.Encrypt, openai.PlainTranslationFields, s.log)
		},
		func(cw *RenewableRequestCipher) forwarder.ResponseMutator {
			return forwarder.WithFullResponseMutation(cw.DecryptResponse)
		},
	)(w, r)
}

func (s *Server) unstructuredHandler(w http.ResponseWriter, r *http.Request) {
	s.inferenceHandler(
		func(cw *RenewableRequestCipher) forwarder.RequestMutator {
			return forwarder.WithFullRequestMutation(cw.Encrypt, s.log)
		},
		func(cw *RenewableRequestCipher) forwarder.ResponseMutator {
			// currently only json response mutation is supported
			return forwarder.WithFullJSONResponseMutation(cw.DecryptResponse, nil, false)
		},
	)(w, r)
}

func (s *Server) noEncryptionHandler(w http.ResponseWriter, r *http.Request) {
	s.setStaticRequestHeaders(r)

	s.forwarder.ForwardWithRetry(
		w, r,
		forwarder.NoRequestMutation,
		forwarder.NoResponseMutation{},
		allowWails,
		forwarder.NoRetry,
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
	r.Header.Set(constants.RequestIDHeader, "proxy_"+uuid.New().String())
	r.Header.Set(constants.PrivatemodeVersionHeader, constants.Version())
	r.Header.Set(constants.PrivatemodeOSHeader, runtime.GOOS)
	r.Header.Set(constants.PrivatemodeArchitectureHeader, runtime.GOARCH)
	r.Header.Set(constants.PrivatemodeClientHeader, s.getClientHeader())
}

// allowWails allows requests from wails (origin wails://wails.localhost)
func allowWails(resp http.Header, req http.Header) error {
	origin := req.Get("Origin")
	if strings.HasPrefix(origin, "wails://") {
		resp.Set("Access-Control-Allow-Origin", origin)
	}
	return nil
}

// setDynamicHeaders sets the dynamic headers for the request.
func (s *Server) setDynamicHeaders(r *http.Request, secret secretmanager.Secret) error {
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
	return nil
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
