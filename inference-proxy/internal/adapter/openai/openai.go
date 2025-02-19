/*
package openai implements an inference API adapter for the [OpenAI API spec].

[OpenAI API sepc]: https://platform.openai.com/docs/api-reference
*/
package openai

import (
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/internal/gpl/openai"
)

// Adapter implements an InferenceAdapter for OpenAI API.
type Adapter struct {
	cipher    *cipher.Cipher
	forwarder mutatingForwarder

	log *slog.Logger
}

// New creates a new InferenceAdapter for the OpenAI API.
func New(cipher *cipher.Cipher, forwarder mutatingForwarder, log *slog.Logger) (*Adapter, error) {
	return &Adapter{
		cipher:    cipher,
		forwarder: forwarder,
		log:       log,
	}, nil
}

// ServeMux returns a multiplexer for intercepting OpenAI API requests.
func (t *Adapter) ServeMux() *http.ServeMux {
	srv := http.NewServeMux()

	// Reject traffic to unsupported endpoints
	srv.HandleFunc("/", t.unsupportedEndpoint)

	// Create chat completion: https://platform.openai.com/docs/api-reference/chat/create
	srv.HandleFunc("/v1/chat/completions", t.forwardWithFieldMutation(
		forwarder.FieldSelector{openai.ChatRequestEncryptionField: forwarder.NestedValue},  // Decrypting should yield an OpenAI response struct
		forwarder.FieldSelector{openai.ChatResponseEncryptionField: forwarder.SimpleValue}, // Encrypting the response field results in a simple string
	)) // cannot restrict to POST method because OPTIONS is needed for CORS by the browser

	// TODO: vllm only supports /v1/chat/completions and /v1/models
	// Until vllm implements more endpoints we won't put effort into implementing these endpoints

	// Create embeddings: https://platform.openai.com/docs/api-reference/embeddings/create
	// srv.HandleFunc("POST /v1/embeddings", t.forwardWithFieldMutation("input", "embedding"))

	// Create fine-tuning job: https://platform.openai.com/docs/api-reference/fine-tuning/create
	// TODO

	// List fine-tuning jobs: https://platform.openai.com/docs/api-reference/fine-tuning/list
	// TODO

	// List fine-tuning events: https://platform.openai.com/docs/api-reference/fine-tuning/list-events
	// TODO

	// List fine-tuning checkpoints: https://platform.openai.com/docs/api-reference/fine-tuning/list-checkpoints
	// TODO

	// Retrieve fine-tuning job: https://platform.openai.com/docs/api-reference/fine-tuning/retrieve
	// TODO

	// Cancel fine-tuning: https://platform.openai.com/docs/api-reference/fine-tuning/cancel
	// TODO

	// Create batch: https://platform.openai.com/docs/api-reference/batch/create
	// TODO

	// Retrieve batch: https://platform.openai.com/docs/api-reference/batch/retrieve
	// TODO

	// Cancel batch: https://platform.openai.com/docs/api-reference/batch/cancel
	// TODO

	// List batch: https://platform.openai.com/docs/api-reference/batch/list
	// TODO

	// Upload file: https://platform.openai.com/docs/api-reference/files/create
	// TODO

	// List files: https://platform.openai.com/docs/api-reference/files/list
	// TODO

	// Retrieve file: https://platform.openai.com/docs/api-reference/files/retrieve
	// TODO

	// Delete file: https://platform.openai.com/docs/api-reference/files/delete
	// TODO

	// Create image: https://platform.openai.com/docs/api-reference/images/create
	// TODO

	// Create image edit: https://platform.openai.com/docs/api-reference/images/createEdit
	// TODO

	// Create image variation: https://platform.openai.com/docs/api-reference/images/createVariation
	// TODO

	// List models: https://platform.openai.com/docs/api-reference/models/list
	srv.HandleFunc("GET /v1/models", t.forwardRequest)

	// Retrieve model: https://platform.openai.com/docs/api-reference/models/retrieve
	// TODO: not yet supported by vLLM
	// srv.HandleFunc("GET /v1/models/{model}", t.forwardRequest)

	// Delete a fine-tuning model: https://platform.openai.com/docs/api-reference/models/delete
	// TODO

	// Create moderation: https://platform.openai.com/docs/api-reference/moderations/create
	// srv.HandleFunc("POST /v1/moderations", t.forwardWithFieldMutation("input", "results"))

	return srv
}

// forwardRequest forwards a request without mutation.
func (t *Adapter) forwardRequest(w http.ResponseWriter, r *http.Request) {
	t.forwarder.Forward(w, r, forwarder.NoRequestMutation, forwarder.NoResponseMutation, forwarder.NoHeaderMutation)
}

// unsupportedEndpoint returns 501 Not Implemented.
// To be used as the default handler for every endpoint that is not explicitly supported.
func (t *Adapter) unsupportedEndpoint(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "unsupported endpoint", http.StatusNotImplemented)
}

// forwardWithFieldMutation returns a handler to forward requests with field mutation using the given selectors.
func (t *Adapter) forwardWithFieldMutation(inputSelector, outputSelector forwarder.FieldSelector) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		session := t.cipher.NewResponseCipher()

		t.forwarder.Forward(
			w, r,
			forwarder.WithJSONRequestMutation(session.DecryptRequest, inputSelector, t.log),
			forwarder.WithJSONResponseMutation(session.EncryptResponse, outputSelector),
			forwarder.NoHeaderMutation,
		)
	}
}

type mutatingForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMutator, forwarder.HeaderMutator)
}
