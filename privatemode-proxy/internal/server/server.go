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
	"time"

	"github.com/edgelesssys/continuum/internal/gpl/auth"
	crypto "github.com/edgelesssys/continuum/internal/gpl/crypto"
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
	LatestSecret(ctx context.Context, now time.Time) (secretmanager.Secret, error)
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
	mux.HandleFunc("/", s.encryptionHandler)
	return mux
}

func (s *Server) encryptionHandler(w http.ResponseWriter, r *http.Request) {
	if s.apiKey != nil {
		r.Header.Set("Authorization", fmt.Sprintf("%s %s", auth.Bearer, *s.apiKey))
	}

	secret, err := s.sm.LatestSecret(r.Context(), time.Now())
	if err != nil {
		forwarder.HTTPError(w, r, http.StatusInternalServerError, "get latest secret: %s", err)
		return
	}
	rc, err := crypto.NewRequestCipher(secret.Data, secret.ID)
	if err != nil {
		forwarder.HTTPError(w, r, http.StatusInternalServerError, "creating request cipher: %s", err)
		return
	}

	inputSelector := forwarder.FieldSelector{
		openai.ChatRequestMessagesField: forwarder.SimpleValue,
		openai.ChatRequestToolsField:    forwarder.SimpleValue,
	} // The encrypted field is a simple string
	outputSelector := forwarder.FieldSelector{openai.ChatResponseEncryptionField: forwarder.NestedValue} // Decrypting should yield an OpenAI response struct
	s.forwarder.Forward(
		w, r,
		forwarder.WithJSONRequestMutation(rc.Encrypt, inputSelector, s.log),
		forwarder.WithJSONResponseMutation(rc.DecryptResponse, outputSelector),
		allowWails,
	)
}

// allowWails allows requests from wails (origin wails://wails.localhost)
func allowWails(resp http.Header, req http.Header) error {
	resp.Del("Access-Control-Allow-Origin")
	origin := req.Get("Origin")
	if strings.HasPrefix(origin, "wails://") {
		resp.Set("Access-Control-Allow-Origin", origin)
	} else {
		resp.Set("Access-Control-Allow-Origin", "null")
	}
	return nil
}
