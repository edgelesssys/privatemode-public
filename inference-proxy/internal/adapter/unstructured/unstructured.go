/*
Package unstructured implements request encryption for the Unstructured API.
*/
package unstructured

import (
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
)

type mutatingForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMutator, forwarder.HeaderMutator, ...forwarder.Opts)
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

// ServeMux returns a ServeMux that forwards requests with encryption.
func (t *Adapter) ServeMux() http.Handler {
	srv := http.NewServeMux()
	srv.HandleFunc("/", t.forwardRequest)
	srv.HandleFunc("/healthcheck", t.forwardHealthcheckRequest)
	return srv
}

func (t *Adapter) forwardRequest(w http.ResponseWriter, r *http.Request) {
	session := t.cipher.NewResponseCipher()
	t.forwarder.Forward(
		w, r,
		forwarder.WithFullRequestMutation(session.DecryptRequest(r.Context()), t.log),
		// currently only JSON responses are supported
		forwarder.WithFullJSONResponseMutation(session.EncryptResponse(r.Context()), nil, false),
		forwarder.NoHeaderMutation,
	)
}

func (t *Adapter) forwardHealthcheckRequest(w http.ResponseWriter, r *http.Request) {
	t.forwarder.Forward(
		w, r,
		forwarder.NoRequestMutation,
		forwarder.NoResponseMutation{},
		forwarder.NoHeaderMutation,
	)
}
