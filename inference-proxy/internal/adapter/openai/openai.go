/*
package openai implements an inference API adapter for the [OpenAI API spec].

[OpenAI API sepc]: https://platform.openai.com/docs/api-reference
*/
package openai

import (
	"fmt"
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
	cipher       *cipher.Cipher
	forwarder    mutatingForwarder
	workloadTask string

	log *slog.Logger
}

// New creates a new InferenceAdapter for the OpenAI API.
func New(workloadTask string, cipher *cipher.Cipher, forwarder mutatingForwarder, log *slog.Logger) (*Adapter, error) {
	return &Adapter{
		cipher:       cipher,
		forwarder:    forwarder,
		workloadTask: workloadTask,
		log:          log,
	}, nil
}

// ServeMux returns a multiplexer for intercepting OpenAI API requests.
func (t *Adapter) ServeMux() *http.ServeMux {
	srv := http.NewServeMux()

	// Reject traffic to unsupported endpoints
	srv.HandleFunc("/", t.unsupportedEndpoint)

	// Create chat completion: https://platform.openai.com/docs/api-reference/chat/create
	srv.HandleFunc("/v1/chat/completions", t.forwardWithFieldMutation(
		forwarder.FieldSelector{{openai.ChatRequestMessagesField}, {openai.ChatRequestToolsField}}, // Decrypting should yield an OpenAI response struct
		openai.PlainCompletionsRequestFields,
		forwarder.FieldSelector{{openai.ChatResponseEncryptionField}}, // Encrypting the response field results in a simple string
		openai.PlainCompletionsResponseFields,
	)) // cannot restrict to POST method because OPTIONS is needed for CORS by the browser

	// Create embeddings: https://platform.openai.com/docs/api-reference/embeddings/create
	srv.HandleFunc("POST /v1/embeddings", t.forwardEmbeddingsRequest)

	// List models: https://platform.openai.com/docs/api-reference/models/list
	srv.HandleFunc("GET /v1/models", t.forwardModelsRequest)

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
		return request + `, "tasks": ["` + t.workloadTask + `"]`, nil
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
		forwarder.WithFullJSONRequestMutation(session.DecryptRequest, openai.PlainEmbeddingsRequestFields, t.log),
		forwarder.WithFullJSONResponseMutation(session.EncryptResponse, openai.PlainEmbeddingsResponseFields, false),
		forwarder.NoHeaderMutation,
	)
}

// forwardWithFieldMutation returns a handler to forward requests with field mutation using the given selectors.
func (t *Adapter) forwardWithFieldMutation(inputEncryptSelector, inputSkipSelector, outputEncryptSelector, outputSkipSelector forwarder.FieldSelector) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		session := t.cipher.NewResponseCipher()

		clientVersion, err := t.getClientSemanticVersion(r)
		if err != nil {
			t.log.Error("retrieving client version", "error", err)
			forwarder.HTTPError(w, r, http.StatusBadRequest, "checking client version: %s", err.Error())
			return
		}

		switch {
		case clientVersion == "": // backwards compatibility for clients < 1.12.0 that didn't set the header
			t.forwarder.Forward(
				w, r,
				forwarder.WithSelectJSONRequestMutation(session.DecryptRequest, inputEncryptSelector, t.log),
				forwarder.WithSelectJSONResponseMutation(session.EncryptResponse, outputEncryptSelector),
				forwarder.NoHeaderMutation,
			)
		case semver.Compare(clientVersion, "v1.16.0") < 0: // backwards compatibility for clients < 1.16.0
			t.forwarder.Forward(
				w, r,
				forwarder.WithFullJSONRequestMutation(session.DecryptRequest, inputSkipSelector, t.log),
				forwarder.WithFullJSONResponseMutation(session.EncryptResponse, outputSkipSelector, true),
				forwarder.NoHeaderMutation,
			)
		default:
			t.forwarder.Forward(
				w, r,
				forwarder.WithFullJSONRequestMutation(session.DecryptRequest, inputSkipSelector, t.log),
				forwarder.WithFullJSONResponseMutation(session.EncryptResponse, outputSkipSelector, false),
				forwarder.NoHeaderMutation,
			)
		}
	}
}

// getClientSemanticVersion returns the client semantic version from the request header.
// NOTE: the app did not set the correct version prior v1.16.0 such that version
// is always 0.0.0 in that case!
func (t *Adapter) getClientSemanticVersion(r *http.Request) (string, error) {
	clientVersion := r.Header.Get(constants.PrivatemodeVersionHeader)

	// Clients without version (< 1.12.0) will not set the header
	if clientVersion != "" {
		// Drop pseudo-version suffix (anything after "-")
		clientVersion, _, _ = strings.Cut(clientVersion, "-")

		// old clients app set the version to "0.0.0", instead of "v0.0.0"
		if clientVersion == "0.0.0" {
			clientVersion = "v0.0.0"
		}

		if !semver.IsValid(clientVersion) {
			return "", fmt.Errorf("invalid client version: %s", clientVersion)
		}
	}

	return clientVersion, nil
}

type mutatingForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMutator, forwarder.HeaderMutator)
}
