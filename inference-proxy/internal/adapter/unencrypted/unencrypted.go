/*
Package unencrypted implements an API adapter for development. It forwards all requests without encryption.

DO NOT USE IN PRODUCTION.
*/
package unencrypted

import (
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/internal/oss/forwarder"
)

type mutatingForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMutator, forwarder.HeaderMutator, ...forwarder.Opts)
}

// Adapter forwards requests without encryption.
type Adapter struct {
	forwarder mutatingForwarder
	log       *slog.Logger
}

// New creates a new UnencryptedAdapter.
func New(forwarder mutatingForwarder, log *slog.Logger) (*Adapter, error) {
	return &Adapter{
		forwarder: forwarder,
		log:       log,
	}, nil
}

// RegisterRoutes registers the unencrypted adapter handlers on the given ServeMux.
// No middleware is applied for unencrypted adapter.
func (t *Adapter) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", t.forwardRequest)
}

// HandlesCatchAll returns true because unencrypted adapter forwards all requests.
func (t *Adapter) HandlesCatchAll() bool {
	return true
}

func (t *Adapter) forwardRequest(w http.ResponseWriter, r *http.Request) {
	t.forwarder.Forward(w, r, forwarder.NoRequestMutation, forwarder.NoResponseMutation{}, forwarder.NoHeaderMutation)
}
