/*
Package openai implements an inference API adapter for the [OpenAI API spec].

[OpenAI API sepc]: https://platform.openai.com/docs/api-reference
*/
package openai

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/inference"
	"github.com/edgelesssys/continuum/internal/compat"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/openai"
)

// Adapter implements an InferenceAdapter for OpenAI API.
type Adapter struct {
	*inference.Adapter
	mutators openai.DefaultRequestMutators
}

// New creates a new InferenceAdapter for the OpenAI API.
func New(workloadTasks []string, cipher inference.ResponseCipherCreator, ocspStatusFile string, forwarder inference.MutatingForwarder, log *slog.Logger) (*Adapter, error) {
	baseAdapter, err := inference.New(workloadTasks, cipher, ocspStatusFile, forwarder, log)
	if err != nil {
		return nil, err
	}

	return &Adapter{
		Adapter:  baseAdapter,
		mutators: openai.GetDefaultRequestMutators(openai.RandomPromptCacheSalt, log),
	}, nil
}

// RegisterRoutes registers the OpenAI API handlers on the given ServeMux.
// Each handler is wrapped with OCSP verification middleware, except for /v1/models
// which is used for health checks and doesn't require GPU attestation.
func (a *Adapter) RegisterRoutes(mux *http.ServeMux) {
	// List models: https://platform.openai.com/docs/api-reference/models/list
	// Not wrapped with OCSP verification - used for health checks
	mux.HandleFunc("GET /v1/models", a.forwardModelsRequest)

	// Create chat completion: https://platform.openai.com/docs/api-reference/chat/create
	mux.Handle("/v1/chat/completions", a.VerifyOCSP(http.HandlerFunc(a.forwardChatCompletionsRequest)))

	// Legacy chat completions endpoint: https://platform.openai.com/docs/api-reference/completions/create
	// Reuse the same handler as for /v1/chat/completions since the unencrypted fields are the same.
	mux.Handle(openai.LegacyCompletionsEndpoint, a.VerifyOCSP(http.HandlerFunc(a.forwardChatCompletionsRequest)))

	// Create embeddings: https://platform.openai.com/docs/api-reference/embeddings/create
	mux.Handle("POST /v1/embeddings", a.VerifyOCSP(http.HandlerFunc(a.forwardEmbeddingsRequest)))

	mux.Handle(openai.TranscriptionsEndpoint, a.VerifyOCSP(http.HandlerFunc(a.forwardTranscriptionsRequest)))
}

// HandlesCatchAll returns false because OpenAI adapter only handles specific endpoints.
func (a *Adapter) HandlesCatchAll() bool {
	return false
}

// forwardModelsRequest forwards a request to the models endpoint of vllm,
// and augments the response with the task vllm is running with.
func (a *Adapter) forwardModelsRequest(w http.ResponseWriter, r *http.Request) {
	mutate := func(request string) (mutatedRequest string, err error) {
		return request + `,"tasks":["` + strings.Join(a.WorkloadTasks, `","`) + `"]`, nil
	}

	a.Forwarder.Forward(
		w, r,
		forwarder.NoRequestMutation,
		forwarder.WithSelectJSONResponseMutation(mutate, forwarder.FieldSelector{{"data", "#", "id"}, {"id"}}), // Mutate both /v1/models and /v1/models/{model}
		forwarder.NoHeaderMutation,
	)
}

func (a *Adapter) forwardEmbeddingsRequest(w http.ResponseWriter, r *http.Request) {
	session := a.Cipher.NewResponseCipher()

	a.Forwarder.Forward(
		w, r,
		forwarder.WithJSONRequestMutation(session.DecryptRequest(r.Context()), openai.PlainEmbeddingsRequestFields, a.Log),
		forwarder.WithJSONResponseMutation(session.EncryptResponse(r.Context()), openai.PlainEmbeddingsResponseFields, false),
		forwarder.NoHeaderMutation,
	)
}

func (a *Adapter) forwardTranscriptionsRequest(w http.ResponseWriter, r *http.Request) {
	session := a.Cipher.NewResponseCipher()

	requestFieldSelector := openai.PlainTranscriptionRequestFields
	responseMutator := forwarder.WithJSONResponseMutation(session.EncryptResponse(r.Context()), openai.PlainTranscriptionResponseFields, false)
	if !compat.AtLeastMajorMinor(r.Header.Get(constants.PrivatemodeVersionHeader), 1, 33) {
		requestFieldSelector = forwarder.FieldSelector{{"model"}}                                 // Clients before v1.33 only have "model" as unencrypted field
		responseMutator = forwarder.WithRawResponseMutation(session.EncryptResponse(r.Context())) // Clients before v1.33 expect the full response body to be encrypted
	}

	a.Forwarder.Forward(
		w, r,
		forwarder.RequestMutatorChain(
			forwarder.WithFormRequestMutation(session.DecryptRequest(r.Context()), requestFieldSelector, a.Log),
			a.mutators.AudioStreamUsageReportingInjector,
		),
		responseMutator,
		forwarder.NoHeaderMutation,
	)
}

// forwardChatCompletionsRequest returns a handler to forward chat completions with field mutation using the given selectors.
func (a *Adapter) forwardChatCompletionsRequest(w http.ResponseWriter, r *http.Request) {
	session := a.Cipher.NewResponseCipher()
	a.Forwarder.Forward(
		w, r,
		forwarder.RequestMutatorChain(
			forwarder.WithJSONRequestMutation(session.DecryptRequest(r.Context()), openai.PlainCompletionsRequestFields, a.Log),
			a.mutators.CacheSaltValidator,
			a.mutators.SecureImageURLValidator,
			a.mutators.StreamUsageReportingInjector,
		),
		forwarder.WithJSONResponseMutation(session.EncryptResponse(r.Context()), openai.PlainCompletionsResponseFields, false),
		forwarder.NoHeaderMutation,
	)
}
