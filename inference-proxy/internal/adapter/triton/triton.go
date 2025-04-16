/*
package triton implements an inference API adapter for Triton Inference Server.

Triton implements the [KServe community standard inference protocol],
as well as several extension defined in the [Triton API spec].

[KServe community standard inference protocol]: https://github.com/kserve/kserve/tree/master/docs/predict-api/v2
[Triton API spec]: https://github.com/triton-inference-server/server/blob/main/docs/protocol
*/
package triton

import (
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
)

// Adapter implements an InferenceAdapter for Triton Inference Server.
type Adapter struct {
	cipher    *cipher.Cipher
	forwarder mutatingForwarder

	log *slog.Logger
}

// New creates a new InferenceAdapter for Triton Inference Server.
func New(cipher *cipher.Cipher, forwarder mutatingForwarder, log *slog.Logger) (*Adapter, error) {
	return &Adapter{
		cipher:    cipher,
		forwarder: forwarder,
		log:       log,
	}, nil
}

// ServeMux returns a multiplexer for intercepting Triton inference requests.
func (t *Adapter) ServeMux() *http.ServeMux {
	srv := http.NewServeMux()

	// Default case: forward request to server
	// TODO: Instead of forwarding any request by default,
	// we should explicitly define the which endpoints we want to forward
	// This is to prevent potentially leaking sensitive information for not properly support endpoints
	srv.HandleFunc("/", t.forwardRequest)

	// Generate request: intercept message and decrypt the `text_input` field
	srv.HandleFunc("POST /v2/models/{modelName}/generate", t.generateHandler())
	srv.HandleFunc("POST /v2/models/{modelName}/versions/{modelVersion}/generate", t.generateHandler())

	return srv
}

func (t *Adapter) generateHandler() func(w http.ResponseWriter, r *http.Request) {
	return t.forwardWithFieldMutation(forwarder.FieldSelector{{"text_input"}}, forwarder.FieldSelector{}, forwarder.FieldSelector{{"text_output"}}, forwarder.FieldSelector{})
}

// forwardRequest forwards a request without mutation.
func (t *Adapter) forwardRequest(w http.ResponseWriter, r *http.Request) {
	t.forwarder.Forward(w, r, forwarder.NoRequestMutation, forwarder.NoResponseMutation, forwarder.NoHeaderMutation)
}

// forwardWithFieldMutation returns a handler to forward requests with field mutation using the given selectors.
func (t *Adapter) forwardWithFieldMutation(inputEncryptSelector, inputSkipSelector, outputEncryptSelector, outputSkipSelector forwarder.FieldSelector) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		session := t.cipher.NewResponseCipher()

		clientVersion := r.Header.Get(constants.PrivatemodeVersionHeader)
		if clientVersion == "" {
			t.forwarder.Forward(
				w, r,
				forwarder.WithSelectJSONRequestMutation(session.DecryptRequest, inputEncryptSelector, t.log),
				forwarder.WithSelectJSONResponseMutation(session.EncryptResponse, outputEncryptSelector),
				forwarder.NoHeaderMutation,
			)
		} else {
			t.forwarder.Forward(
				w, r,
				forwarder.WithFullJSONRequestMutation(session.DecryptRequest, inputSkipSelector, t.log),
				forwarder.WithFullJSONResponseMutation(session.EncryptResponse, outputSkipSelector),
				forwarder.NoHeaderMutation,
			)
		}
	}
}

type mutatingForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMutator, forwarder.HeaderMutator)
}
