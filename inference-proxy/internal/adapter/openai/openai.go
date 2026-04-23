/*
Package openai implements an inference API adapter for the [OpenAI API spec].

[OpenAI API sepc]: https://platform.openai.com/docs/api-reference
*/
package openai

import (
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/inference"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
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
	mux.HandleFunc("GET /v1/models/{model}", a.forwardSpecificModelRequest) // Also handle model details endpoint

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
		result := request
		data := gjson.Get(request, "data")
		if !data.Exists() || !data.IsArray() {
			return request, nil
		}

		var mutateErr error
		data.ForEach(func(key, _ gjson.Result) bool {
			path := "data." + key.String() + ".tasks"
			result, mutateErr = sjson.Set(result, path, a.WorkloadTasks)
			return mutateErr == nil // continue if no error
		})

		return result, mutateErr
	}
	a.Forwarder.Forward(
		w, r,
		forwarder.NoRequestMutation,
		forwarder.RawResponseMapper(mutate),
	)
}

func (a *Adapter) forwardSpecificModelRequest(w http.ResponseWriter, r *http.Request) {
	mutate := func(request string) (mutatedRequest string, err error) {
		return sjson.Set(request, "tasks", a.WorkloadTasks)
	}
	a.Forwarder.Forward(
		w, r,
		forwarder.NoRequestMutation,
		forwarder.RawResponseMapper(mutate),
	)
}

func (a *Adapter) forwardEmbeddingsRequest(w http.ResponseWriter, r *http.Request) {
	session := a.Cipher.NewResponseCipher()

	a.Forwarder.Forward(
		w, r,
		forwarder.WithJSONRequestMutation(session.DecryptRequest(r.Context()), openai.PlainEmbeddingsRequestFields, a.Log),
		forwarder.JSONResponseMapper(session.EncryptResponse(r.Context()), openai.PlainEmbeddingsResponseFields),
	)
}

func (a *Adapter) forwardTranscriptionsRequest(w http.ResponseWriter, r *http.Request) {
	session := a.Cipher.NewResponseCipher()

	a.Forwarder.Forward(
		w, r,
		forwarder.RequestMutatorChain(
			forwarder.WithFormRequestMutation(session.DecryptRequest(r.Context()), openai.PlainTranscriptionRequestFields, a.Log),
			a.mutators.AudioStreamUsageReportingInjector,
		),
		forwarder.JSONResponseMapper(session.EncryptResponse(r.Context()), openai.PlainTranscriptionResponseFields),
	)
}

// forwardChatCompletionsRequest returns a handler to forward chat completions with field mutation using the given selectors.
func (a *Adapter) forwardChatCompletionsRequest(w http.ResponseWriter, r *http.Request) {
	session := a.Cipher.NewResponseCipher()

	// duplicate reasoning field during deprecation
	// TODO: remove after May 1st
	responseMutation := forwarder.MutationFuncChain(duplicateReasoningFieldInJSON, session.EncryptResponse(r.Context()))

	a.Forwarder.Forward(
		w, r,
		forwarder.RequestMutatorChain(
			forwarder.WithJSONRequestMutation(session.DecryptRequest(r.Context()), openai.PlainCompletionsRequestFields, a.Log),
			a.mutators.CacheSaltValidator,
			a.mutators.MediaContentValidator,
			a.mutators.StreamUsageReportingInjector,
		),
		forwarder.JSONResponseMapper(responseMutation, openai.PlainCompletionsResponseFields),
	)
}

func duplicateReasoningFieldInJSON(in string) (string, error) {
	data := []byte(in)
	if len(data) == 0 || !gjson.ValidBytes(data) {
		return in, nil
	}

	result := gjson.ParseBytes(data)
	if !result.IsArray() {
		return in, nil
	}

	var mutateErr error
	result.ForEach(func(key, _ gjson.Result) bool {
		for _, field := range []string{"message", "delta"} {
			reasoningPath := key.String() + "." + field + ".reasoning"
			reasoning := gjson.GetBytes(data, reasoningPath)
			if !reasoning.Exists() {
				continue
			}

			data, mutateErr = sjson.SetRawBytes(data, key.String()+"."+field+".reasoning_content", []byte(reasoning.Raw))
			if mutateErr != nil {
				return false
			}
		}
		return true
	})
	if mutateErr != nil {
		return "", mutateErr
	}

	return string(data), nil
}
