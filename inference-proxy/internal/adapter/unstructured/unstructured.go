/*
Package unstructured implements request encryption for the Unstructured API.
*/
package unstructured

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/oss/compat"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/encbody"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
)

type mutatingForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMapper, ...forwarder.Opts)
}

// Adapter forwards requests with encryption.
type Adapter struct {
	cipher    *cipher.Cipher
	forwarder mutatingForwarder
	log       *slog.Logger
}

// New creates a new UnstructuredAdapter.
func New(cipher *cipher.Cipher, forwarder mutatingForwarder, log *slog.Logger) (*Adapter, error) {
	return &Adapter{
		cipher:    cipher,
		forwarder: forwarder,
		log:       log,
	}, nil
}

// RegisterRoutes registers the Unstructured API handlers on the given ServeMux.
// No OCSP verification middleware is applied for Unstructured API.
func (t *Adapter) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", t.forwardRequest)
	mux.HandleFunc("/healthcheck", t.forwardHealthcheckRequest)
}

// HandlesCatchAll returns true because Unstructured adapter forwards all requests.
func (t *Adapter) HandlesCatchAll() bool {
	return true
}

func (t *Adapter) forwardRequest(w http.ResponseWriter, r *http.Request) {
	if compat.AtLeastMajorMinor(r.Header.Get(constants.PrivatemodeVersionHeader), 1, 46) {
		t.forwardRequestFBE(w, r)
	} else {
		t.forwardRequestLegacy(w, r)
	}
}

func (t *Adapter) forwardRequestLegacy(w http.ResponseWriter, r *http.Request) {
	session := t.cipher.NewResponseCipher()
	t.forwarder.Forward(
		w, r,
		forwarder.WithRawRequestMutation(session.DecryptRequest(r.Context()), t.log),
		// currently only JSON responses are supported
		forwarder.JSONResponseMapper(session.EncryptResponse(r.Context()), nil),
	)
}

func (t *Adapter) forwardRequestFBE(w http.ResponseWriter, r *http.Request) {
	// Set by every successful execution of the request mutator
	var exchange *encbody.ServerExchange

	if !encbody.IsEncrypted(r.Header) {
		forwarder.HTTPError(w, r, 400, "request must be encrypted")
		return
	}
	secretID := r.Header.Get(constants.PrivatemodeSecretIDHeader)
	if secretID == "" {
		forwarder.HTTPError(w, r, 400, "request must contain secret ID")
		return
	}
	secret, err := t.cipher.Secret(r.Context(), secretID)
	if err != nil {
		forwarder.HTTPError(w, r, 400, "getting secret: %v", err)
		return
	}

	t.forwarder.Forward(
		w, r,
		func(r *http.Request) error {
			var err error
			exchange, err = encbody.InitializeServerExchange(secret, r)
			if err != nil {
				return fmt.Errorf("initializing server exchange: %w", err)
			}
			return exchange.DecryptRequest(r)
		},
		func(resp *http.Response) (forwarder.Response, error) {
			dsResp, err := forwarder.ReadUnaryResponse(resp, constants.MaxUnaryResponseBodyBytes)
			if err != nil {
				return nil, fmt.Errorf("reading upstream response body: %w", err)
			}
			dsResp.Header.Set("Content-Type", resp.Header.Get("Content-Type"))
			if err := exchange.EncryptUnaryResponse(dsResp); err != nil {
				return nil, fmt.Errorf("FBE encrypting response: %w", err)
			}
			return dsResp, nil
		},
	)
}

func (t *Adapter) forwardHealthcheckRequest(w http.ResponseWriter, r *http.Request) {
	t.forwarder.Forward(
		w, r,
		forwarder.NoRequestMutation,
		forwarder.PassthroughResponseMapper,
	)
}
