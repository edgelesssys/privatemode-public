/*
Package openai implements an inference API adapter for the [OpenAI API spec].

[OpenAI API sepc]: https://platform.openai.com/docs/api-reference
*/
package openai

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/internal/gpl/openai"
	"golang.org/x/mod/semver"
)

// Adapter implements an InferenceAdapter for OpenAI API.
type Adapter struct {
	cipher        *cipher.Cipher
	forwarder     mutatingForwarder
	workloadTasks []string

	saltValidator           forwarder.RequestMutator
	saltInjector            forwarder.RequestMutator
	secureImageURLValidator forwarder.RequestMutator

	log *slog.Logger
}

// New creates a new InferenceAdapter for the OpenAI API.
func New(workloadTasks []string, cipher *cipher.Cipher, forwarder mutatingForwarder, log *slog.Logger) (*Adapter, error) {
	if len(workloadTasks) == 0 {
		return nil, errors.New("no workload tasks provided")
	}

	return &Adapter{
		cipher:                  cipher,
		forwarder:               forwarder,
		workloadTasks:           workloadTasks,
		saltInjector:            openai.CacheSaltInjector(openai.RandomPromptCacheSalt, log),
		saltValidator:           openai.CacheSaltValidator(log),
		secureImageURLValidator: openai.SecureImageURLValidator(log),
		log:                     log,
	}, nil
}

// ServeMux returns a multiplexer for intercepting OpenAI API requests.
func (t *Adapter) ServeMux() *http.ServeMux {
	srv := http.NewServeMux()

	// Reject traffic to unsupported endpoints
	srv.HandleFunc("/", t.unsupportedEndpoint)

	// Create chat completion: https://platform.openai.com/docs/api-reference/chat/create
	srv.HandleFunc("/v1/chat/completions", t.forwardChatCompletionsRequest()) // cannot restrict to POST method because OPTIONS is needed for CORS by the browser

	// Legacy chat completions endpoint: https://platform.openai.com/docs/api-reference/completions/create
	// Reuse the same handler as for /v1/chat/completions since the unencrypted fields are the same.
	srv.HandleFunc(openai.LegacyCompletionsEndpoint, t.forwardChatCompletionsRequest())

	// Create embeddings: https://platform.openai.com/docs/api-reference/embeddings/create
	srv.HandleFunc("POST /v1/embeddings", t.forwardEmbeddingsRequest)

	// List models: https://platform.openai.com/docs/api-reference/models/list
	srv.HandleFunc("GET /v1/models", t.forwardModelsRequest)

	srv.HandleFunc(openai.TranscriptionsEndpoint, t.forwardTranscriptionsRequest)

	srv.HandleFunc(openai.TranslationsEndpoint, t.forwardTranslationsRequest)

	// TODO: vllm only supports /v1/chat/completions and /v1/models
	// Until vllm implements more endpoints we won't put effort into implementing these endpoints

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

	// Retrieve model: https://platform.openai.com/docs/api-reference/models/retrieve
	// TODO: not yet supported by vLLM
	// srv.HandleFunc("GET /v1/models/{model}", t.forwardRequest)

	// Delete a fine-tuning model: https://platform.openai.com/docs/api-reference/models/delete
	// TODO

	// Create moderation: https://platform.openai.com/docs/api-reference/moderations/create
	// srv.HandleFunc("POST /v1/moderations", t.forwardWithFieldMutation("input", "results"))

	return srv
}

// unsupportedEndpoint returns 501 Not Implemented.
// To be used as the default handler for every endpoint that is not explicitly supported.
func (t *Adapter) unsupportedEndpoint(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "unsupported endpoint", http.StatusNotImplemented)
}

// forwardModelsRequest forwards a request to the models endpoint of vllm,
// and augments the response with the task vllm is running with.
func (t *Adapter) forwardModelsRequest(w http.ResponseWriter, r *http.Request) {
	mutate := func(request string) (mutatedRequest string, err error) {
		return request + `,"tasks":["` + strings.Join(t.workloadTasks, `","`) + `"]`, nil
	}

	t.forwarder.Forward(
		w, r,
		forwarder.NoRequestMutation,
		forwarder.WithSelectJSONResponseMutation(mutate, forwarder.FieldSelector{{"data", "#", "id"}, {"id"}}), // Mutate both /v1/models and /v1/models/{model}
		forwarder.NoHeaderMutation,
	)
}

func (t *Adapter) forwardEmbeddingsRequest(w http.ResponseWriter, r *http.Request) {
	session := t.cipher.NewResponseCipher()

	t.forwarder.Forward(
		w, r,
		forwarder.WithFullJSONRequestMutation(session.DecryptRequest(r.Context()), openai.PlainEmbeddingsRequestFields, t.log),
		forwarder.WithFullJSONResponseMutation(session.EncryptResponse(r.Context()), openai.PlainEmbeddingsResponseFields, false),
		forwarder.NoHeaderMutation,
	)
}

func (t *Adapter) forwardTranscriptionsRequest(w http.ResponseWriter, r *http.Request) {
	session := t.cipher.NewResponseCipher()

	t.forwarder.Forward(
		w, r,
		forwarder.WithFormRequestMutation(session.DecryptRequest(r.Context()), openai.PlainTranscriptionFields, t.log),
		forwarder.WithFullResponseMutation(session.EncryptResponse(r.Context())),
		forwarder.NoHeaderMutation,
	)
}

func (t *Adapter) forwardTranslationsRequest(w http.ResponseWriter, r *http.Request) {
	session := t.cipher.NewResponseCipher()

	t.forwarder.Forward(
		w, r,
		forwarder.WithFormRequestMutation(session.DecryptRequest(r.Context()), openai.PlainTranslationFields, t.log),
		forwarder.WithFullResponseMutation(session.EncryptResponse(r.Context())),
		forwarder.NoHeaderMutation,
	)
}

// forwardChatCompletionsRequest returns a handler to forward chat completions with field mutation using the given selectors.
func (t *Adapter) forwardChatCompletionsRequest() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		session := t.cipher.NewResponseCipher()

		saltMutator := t.saltValidator
		clientVersion := r.Header.Get(constants.PrivatemodeVersionHeader)
		if semver.Compare(clientVersion, "v1.17.0") < 0 { // clients without cache_salt
			saltMutator = t.saltInjector
		}
		t.forwarder.Forward(
			w, r,
			forwarder.RequestMutatorChain(
				forwarder.WithFullJSONRequestMutation(session.DecryptRequest(r.Context()), openai.PlainCompletionsRequestFields, t.log),
				saltMutator,
				t.secureImageURLValidator,
			),
			forwarder.WithFullJSONResponseMutation(session.EncryptResponse(r.Context()), openai.PlainCompletionsResponseFields, false),
			forwarder.NoHeaderMutation,
		)
	}
}

type mutatingForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMutator, forwarder.HeaderMutator)
}
