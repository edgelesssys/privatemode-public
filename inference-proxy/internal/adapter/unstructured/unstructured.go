/*
Package unstructured implements request encryption for the Unstructured API.
*/
package unstructured

import (
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
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
	session := t.cipher.NewResponseCipher()
	t.forwarder.Forward(
		w, r,
		forwarder.WithRawRequestMutation(session.DecryptRequest(r.Context()), t.log),
		// currently only JSON responses are supported
		forwarder.JSONResponseMapper(session.EncryptResponse(r.Context()), nil),
	)
}

func (t *Adapter) forwardHealthcheckRequest(w http.ResponseWriter, r *http.Request) {
	t.forwarder.Forward(
		w, r,
		forwarder.NoRequestMutation,
		forwarder.PassthroughResponseMapper,
	)
}
