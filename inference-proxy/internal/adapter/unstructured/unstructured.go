/*
Package unstructured implements request encryption for the Unstructured API.
*/
package unstructured

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/inference-proxy/internal/encforward"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
)

// Adapter forwards requests with encryption.
type Adapter struct {
	encForwarder encforward.Forwarder
	log          *slog.Logger
}

// New creates a new UnstructuredAdapter.
func New(encForwarder encforward.Forwarder, log *slog.Logger) (*Adapter, error) {
	return &Adapter{
		encForwarder: encForwarder,
		log:          log,
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
	ctx := r.Context()
	t.encForwarder.ForwardEncrypted(
		w, r,
		func(s cipher.ResponseCipher) forwarder.RequestMutator {
			return forwarder.WithRawRequestMutation(s.DecryptRequest(ctx), t.log)
		},
		forwarder.NoRequestMutation,
		nil, // No response fields are left in plaintext.
		func(resp *http.Response) (forwarder.Response, error) {
			// All responses are unary JSON
			dsResp, err := forwarder.ReadUnaryResponse(resp, constants.MaxUnaryResponseBodyBytes)
			if err != nil {
				return nil, fmt.Errorf("reading upstream response body: %w", err)
			}
			dsResp.Header.Set("Content-Type", resp.Header.Get("Content-Type"))
			return dsResp, nil
		},
	)
}

func (t *Adapter) forwardHealthcheckRequest(w http.ResponseWriter, r *http.Request) {
	t.encForwarder.ForwardPlain(
		w, r,
		forwarder.NoRequestMutation,
		forwarder.PassthroughResponseMapper,
	)
}
