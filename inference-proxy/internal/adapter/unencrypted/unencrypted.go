/*
package unencrypted implements an API adapter for development. It forwards all requests without encryption.

DO NOT USE IN PRODUCTION.
*/
package unencrypted

import (
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
)

type mutatingForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMutator, forwarder.HeaderMutator)
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

// ServeMux returns a ServeMux that forwards requests without encryption.
func (t *Adapter) ServeMux() *http.ServeMux {
	srv := http.NewServeMux()
	srv.HandleFunc("/", t.forwardRequest)
	return srv
}

func (t *Adapter) forwardRequest(w http.ResponseWriter, r *http.Request) {
	t.forwarder.Forward(w, r, forwarder.NoRequestMutation, forwarder.NoResponseMutation, forwarder.NoHeaderMutation)
}
