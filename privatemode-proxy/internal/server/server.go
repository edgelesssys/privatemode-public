// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// package server implements the HTTP server to forward encrypted requests to the API.
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/edgelesssys/continuum/internal/gpl/auth"
	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/crypto"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/internal/gpl/logging"
	"github.com/edgelesssys/continuum/internal/gpl/openai"
	"github.com/edgelesssys/continuum/internal/gpl/process"
	"github.com/edgelesssys/continuum/internal/gpl/secretmanager"
)

// Server implements the HTTP server for the API gateway.
type Server struct {
	apiKey    *string
	forwarder apiForwarder
	sm        secretManager
	log       *slog.Logger
}

type apiForwarder interface {
	Forward(
		w http.ResponseWriter, req *http.Request,
		requestMutator forwarder.RequestMutator, responseMutator forwarder.ResponseMutator, headerMutator forwarder.HeaderMutator,
	)
}

type secretManager interface {
	LatestSecret(ctx context.Context) (secretmanager.Secret, error)
}

// New sets up a new Server.
func New(
	client *http.Client, apiEndpoint string, protocolScheme forwarder.ProtocolScheme, sm secretManager, log *slog.Logger, apiKey *string,
) *Server {
	return &Server{
		apiKey:    apiKey,
		forwarder: forwarder.NewWithClient(client, apiEndpoint, protocolScheme, log),
		sm:        sm,
		log:       log,
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
	mux.HandleFunc("/unstructured/", s.unstructuredHandler)
	mux.HandleFunc(openai.ModelsEndpoint, s.noEncryptionHandler)
	mux.HandleFunc(openai.EmbeddingsEndpoint, s.embeddingsHandler)

	mux.HandleFunc("/", http.NotFound) // Reject requests to unknown endpoints

	return mux
}

func (s *Server) chatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	s.setRequestHeaders(r)

	rc, err := s.getRequestCipher(r)
	if err != nil {
		forwarder.HTTPError(w, r, http.StatusInternalServerError, "creating request cipher: %s", err)
		return
	}

	s.forwarder.Forward(
		w, r,
		forwarder.WithFullJSONRequestMutation(rc.Encrypt, openai.PlainCompletionsRequestFields, s.log),
		forwarder.WithFullJSONResponseMutation(rc.DecryptResponse, openai.PlainCompletionsResponseFields, false),
		allowWails,
	)
}

func (s *Server) embeddingsHandler(w http.ResponseWriter, r *http.Request) {
	s.setRequestHeaders(r)

	rc, err := s.getRequestCipher(r)
	if err != nil {
		forwarder.HTTPError(w, r, http.StatusInternalServerError, "creating request cipher: %s", err)
		return
	}

	s.forwarder.Forward(
		w, r,
		forwarder.WithFullJSONRequestMutation(rc.Encrypt, openai.PlainEmbeddingsRequestFields, s.log),
		forwarder.WithFullJSONResponseMutation(rc.DecryptResponse, openai.PlainEmbeddingsResponseFields, false),
		allowWails,
	)
}

func (s *Server) unstructuredHandler(w http.ResponseWriter, r *http.Request) {
	s.setRequestHeaders(r)

	rc, err := s.getRequestCipher(r)
	if err != nil {
		forwarder.HTTPError(w, r, http.StatusInternalServerError, "creating request cipher: %s", err)
		return
	}

	s.forwarder.Forward(
		w, r,
		forwarder.WithFullRequestMutation(rc.Encrypt, s.log),
		// currently only json response mutation is supported
		forwarder.WithFullJSONResponseMutation(rc.DecryptResponse, nil, false),
		allowWails,
	)
}

func (s *Server) noEncryptionHandler(w http.ResponseWriter, r *http.Request) {
	s.setRequestHeaders(r)

	s.forwarder.Forward(
		w, r,
		forwarder.NoRequestMutation,
		forwarder.NoResponseMutation{},
		allowWails,
	)
}

func (s *Server) getRequestCipher(r *http.Request) (*crypto.RequestCipher, error) {
	secret, err := s.sm.LatestSecret(r.Context())
	if err != nil {
		return nil, fmt.Errorf("get latest secret: %w", err)
	}

	rc, err := crypto.NewRequestCipher(secret.Data, secret.ID)
	if err != nil {
		return nil, fmt.Errorf("creating request cipher: %w", err)
	}
	return rc, nil
}

func (s *Server) setRequestHeaders(r *http.Request) {
	if s.apiKey != nil {
		r.Header.Set("Authorization", fmt.Sprintf("%s %s", auth.Bearer, *s.apiKey))
	}
	r.Header.Set(constants.PrivatemodeVersionHeader, constants.Version())
}

// allowWails allows requests from wails (origin wails://wails.localhost)
func allowWails(resp http.Header, req http.Header) error {
	origin := req.Get("Origin")
	if strings.HasPrefix(origin, "wails://") {
		resp.Set("Access-Control-Allow-Origin", origin)
	}
	return nil
}
