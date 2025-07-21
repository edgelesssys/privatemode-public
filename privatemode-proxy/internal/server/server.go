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
	"github.com/edgelesssys/continuum/internal/gpl/openai"
	"github.com/edgelesssys/continuum/internal/gpl/process"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// Server implements the HTTP server for the API gateway.
type Server struct {
	apiKey           *string
	defaultCacheSalt string // if no salt is set, a random salt will be used
	forwarder        apiForwarder
	sm               secretManager
	log              *slog.Logger
	isApp            bool
}

type apiForwarder interface {
	ForwardWithRetry(
		w http.ResponseWriter, req *http.Request,
		requestMutator forwarder.RequestMutator, responseMutator forwarder.ResponseMutator,
		headerMutator forwarder.HeaderMutator, retryCallback forwarder.RetryCallback,
	)
}

// New sets up a new Server.
func New(
	client *http.Client, apiEndpoint string, protocolScheme forwarder.ProtocolScheme, sm secretManager,
	log *slog.Logger, apiKey *string, promptCacheSalt string, isApp bool,
) *Server {
	log.Info("version", slog.String("version", constants.Version()))
	fwd := forwarder.NewWithClient(client, apiEndpoint, protocolScheme, log)

	return &Server{
		apiKey:           apiKey,
		defaultCacheSalt: promptCacheSalt,
		forwarder:        fwd,
		sm:               sm,
		log:              log,
		isApp:            isApp,
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

func (s *Server) cacheSaltInjector() forwarder.RequestMutator {
	var cacheSaltGenerator func() (string, error)
	if s.defaultCacheSalt == "" {
		cacheSaltGenerator = openai.RandomPromptCacheSalt
	} else {
		cacheSaltGenerator = func() (string, error) {
			return s.defaultCacheSalt, nil
		}
	}

	return openai.CacheSaltInjector(cacheSaltGenerator, s.log)
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
		if cacheSalt == "" {
			return fmt.Errorf("missing field 'cache_salt'")
		}

		hash := sha256.Sum256([]byte(cacheSalt))
		shardKey := hex.EncodeToString(hash[16:])
		r.Header.Set(constants.PrivatemodeShardKeyHeader, shardKey)
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		return nil
	}
}

func (s *Server) inferenceHandler(
	requestMutator func(*RenewableRequestCipher) forwarder.RequestMutator, responseMutator func(*RenewableRequestCipher) forwarder.ResponseMutator,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		s.setRequestHeaders(r)

		rc, err := NewRenewableRequestCipher(s.sm, r)
		if err != nil {
			forwarder.HTTPError(w, r, http.StatusInternalServerError, "creating request cipher: %s", err)
			return
		}

		// Set up retry logic for specific status codes with exponential backoff
		retryCallback := func(statusCode int, body []byte, attempt int) (bool, time.Duration) {
			shouldRetry := statusCode == 500 && attempt <= 1 && strings.Contains(string(body), constants.ErrorNoSecretForID)
			if !shouldRetry {
				return false, 0
			}

			// Force a new rc for the next attempt
			if err := rc.ResetSecret(r); err != nil {
				s.log.Error("resetting request cipher", "error", err)
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
				s.cacheSaltInjector(),
				s.shardKeyInjector(),
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
	s.setRequestHeaders(r)

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

func (s *Server) setRequestHeaders(r *http.Request) {
	if s.apiKey != nil {
		r.Header.Set("Authorization", fmt.Sprintf("%s %s", auth.Bearer, *s.apiKey))
	}
	requestID := "proxy_" + uuid.New().String()
	r.Header.Set(constants.RequestIDHeader, requestID)
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
