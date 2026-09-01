// Package userapi handles user requests to the secret-service.
package userapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/hpke"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/edgelesssys/continuum/internal/mtls"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/httpapi"
	"github.com/edgelesssys/continuum/internal/oss/process"
	"github.com/edgelesssys/continuum/internal/oss/secretclient"
	"github.com/edgelesssys/continuum/internal/oss/secretexchange"
)

// Server handles communication with users.
type Server struct {
	mtlsIdentity mtls.Identity
	secretStore  secretSetter
	log          *slog.Logger
	meshCertRaw  []byte
	meshPriv     *ecdsa.PrivateKey
}

// New returns a new Server for the user API.
func New(meshTLSConfig *tls.Config, mtlsIdentity mtls.Identity, secretStore secretSetter, logger *slog.Logger) (*Server, error) {
	if meshTLSConfig == nil || len(meshTLSConfig.Certificates) != 1 {
		return nil, errors.New("expected a mesh TLS config with exactly one certificate chain")
	}
	tlsCertChain := meshTLSConfig.Certificates[0]
	priv, ok := tlsCertChain.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("expected an ECDSA private key in the TLS certificate")
	}

	return &Server{
		mtlsIdentity: mtlsIdentity,
		secretStore:  secretStore,
		log:          logger,
		meshCertRaw:  tlsCertChain.Certificate[0],
		meshPriv:     priv,
	}, nil
}

// Serve starts the server on the given endpoint.
func (s *Server) Serve(ctx context.Context, endpoint string) error {
	lis, err := net.Listen("tcp", endpoint)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	server := &http.Server{
		Addr:      endpoint,
		ErrorLog:  slog.NewLogLogger(s.log.Handler(), slog.LevelError),
		Handler:   s.handler(),
		TLSConfig: s.mtlsIdentity.ServerConfig(),
	}
	return process.HTTPServeContext(ctx, server, lis, s.log)
}

// handler returns the HTTP handler for the user API.
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+secretclient.SecretPath, s.exchangeSecretHandler)
	return mux
}

func (s *Server) exchangeSecretHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, secretclient.MaxRequestBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, fmt.Sprintf("reading request: %s", err), http.StatusBadRequest)
		return
	}

	var req httpapi.SecretReq
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("decoding request: %s", err), http.StatusBadRequest)
		return
	}
	resp, err := s.exchangeSecret(r.Context(), &req)
	if err != nil {
		if requestErr, ok := errors.AsType[*requestError](err); ok {
			http.Error(w, requestErr.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.log.ErrorContext(r.Context(), "Encoding response", "error", err)
	}
}

// exchangeSecret performs a cryptographic key agreement.
func (s *Server) exchangeSecret(ctx context.Context, req *httpapi.SecretReq) (*httpapi.SecretResp, error) {
	// Create HPKE encapsulated key and sender context.
	pub, err := hpke.MLKEM768X25519().NewPublicKey(req.PublicKey)
	if err != nil {
		return nil, &requestError{fmt.Errorf("parsing public key: %w", err)}
	}
	encapKey, sender, err := hpke.NewSender(pub, hpke.HKDFSHA256(), hpke.ExportOnly(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating HPKE sender: %w", err)
	}

	// Sign the request (to prove freshness) and the response (to prove that it has been generated inside the TEE) with the mesh private key.
	signature, err := ecdsa.SignASN1(rand.Reader, s.meshPriv, secretexchange.Hash(req.PublicKey, encapKey))
	if err != nil {
		return nil, fmt.Errorf("signing: %w", err)
	}

	// Store the shared secret.
	secret, err := sender.Export("", 32)
	if err != nil {
		return nil, fmt.Errorf("exporting secret: %w", err)
	}
	secrets := map[string][]byte{secretexchange.ID(req.PublicKey): secret}
	if err := s.secretStore.SetSecrets(ctx, secrets, int64(constants.InferenceSecretTTL/time.Second)); err != nil {
		return nil, fmt.Errorf("saving secrets: %w", err)
	}

	return &httpapi.SecretResp{
		EncapsulatedKey: encapKey,
		Signature:       signature,
		MeshCert:        s.meshCertRaw,
	}, nil
}

type requestError struct {
	err error
}

func (e *requestError) Error() string { return e.err.Error() }
func (e *requestError) Unwrap() error { return e.err }

type secretSetter interface {
	SetSecrets(context.Context, map[string][]byte, int64) error
}
