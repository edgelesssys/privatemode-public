/*
Package openai implements an inference API adapter for the [OpenAI API spec].

[OpenAI API sepc]: https://platform.openai.com/docs/api-reference
*/
package openai

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/inference"
	"github.com/edgelesssys/continuum/internal/model"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/usage"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Adapter implements an InferenceAdapter for OpenAI API.
type Adapter struct {
	*inference.Adapter
	mutators openai.DefaultRequestMutators
}

// New creates a new InferenceAdapter for the OpenAI API.
func New(workloadTasks []string, cipher inference.ResponseCipherCreator, ocspStatusFile string, forwarder inference.MutatingForwarder, reporter inference.UsageReporter, log *slog.Logger) (*Adapter, error) {
	baseAdapter, err := inference.New(workloadTasks, cipher, ocspStatusFile, forwarder, reporter, log)
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
	encryptMutator := forwarder.NewJSONMutatingReader(session.EncryptResponse(r.Context()), openai.PlainEmbeddingsResponseFields)

	a.Forwarder.Forward(
		w, r,
		forwarder.WithJSONRequestMutation(session.DecryptRequest(r.Context()), openai.PlainEmbeddingsRequestFields, a.Log),
		a.ResponseMapper(r, encryptMutator, extractOpenAIUsage, extractOpenAIUsage),
	)
}

func (a *Adapter) forwardTranscriptionsRequest(w http.ResponseWriter, r *http.Request) {
	session := a.Cipher.NewResponseCipher()
	encryptMutator := forwarder.NewJSONMutatingReader(session.EncryptResponse(r.Context()), openai.PlainTranscriptionResponseFields)

	a.Forwarder.Forward(
		w, r,
		forwarder.RequestMutatorChain(
			forwarder.WithFormRequestMutation(session.DecryptRequest(r.Context()), openai.PlainTranscriptionRequestFields, a.Log),
			a.mutators.AudioStreamUsageReportingInjector,
		),
		a.ResponseMapper(r, encryptMutator, extractUnaryTranscriptionUsage, a.extractStreamingTranscriptionUsage),
	)
}

// forwardChatCompletionsRequest forwards chat completions with field mutation using the given selectors.
func (a *Adapter) forwardChatCompletionsRequest(w http.ResponseWriter, r *http.Request) {
	session := a.Cipher.NewResponseCipher()

	encryptMutator := forwarder.NewJSONMutatingReader(
		// duplicate reasoning field during deprecation
		// TODO: remove after May 1st
		forwarder.MutationFuncChain(duplicateReasoningFieldInJSON, session.EncryptResponse(r.Context())),
		openai.PlainCompletionsResponseFields,
	)

	a.Forwarder.Forward(
		w, r,
		forwarder.RequestMutatorChain(
			forwarder.WithJSONRequestMutation(session.DecryptRequest(r.Context()), openai.PlainCompletionsRequestFields, a.Log),
			a.mutators.CacheSaltValidator,
			a.mutators.MediaContentValidator,
			a.mutators.StreamUsageReportingInjector,
		),
		a.ResponseMapper(r, encryptMutator, extractOpenAIUsage, extractOpenAIUsage),
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

func extractOpenAIUsage(body []byte) (usage.Stats, error) {
	var parsed struct {
		Usage *openai.Usage `json:"usage"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return usage.Stats{}, err
	}
	if parsed.Usage == nil {
		return usage.Stats{}, inference.ErrNoUsage
	}
	return parsed.Usage.ToUsageStats(), nil
}

func extractUnaryTranscriptionUsage(body []byte) (usage.Stats, error) {
	var resp openai.TranscriptionUsageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return usage.Stats{}, err
	}

	audioSeconds := int64(max(resp.Duration, resp.Usage.Seconds))
	if audioSeconds <= 0 {
		return usage.Stats{}, inference.ErrNoUsage
	}

	return usage.Stats{AudioSeconds: audioSeconds}, nil
}

func (a *Adapter) extractStreamingTranscriptionUsage(body []byte) (usage.Stats, error) {
	var parsed struct {
		Model string        `json:"model"`
		Usage *openai.Usage `json:"usage"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return usage.Stats{}, err
	}
	if parsed.Usage == nil {
		return usage.Stats{}, inference.ErrNoUsage
	}

	baseUsage := parsed.Usage.ToUsageStats()
	modelName := model.Resolve(parsed.Model)

	var audioSeconds int64
	switch modelName {
	case model.OpenAIWhisperLargeV3:
		audioSeconds = baseUsage.PromptTokens / model.TokensPerAudioSecondWhisper
	case model.VoxtralMini3b:
		audioSeconds = (baseUsage.PromptTokens - model.TokenOverheadVoxtral) / model.TokensPerAudioSecondVoxtral
	default:
		a.Log.Warn("Unknown transcription model, falling back to Voxtral billing formula", "model", parsed.Model)
		audioSeconds = (baseUsage.PromptTokens - model.TokenOverheadVoxtral) / model.TokensPerAudioSecondVoxtral
	}

	return usage.Stats{AudioSeconds: max(0, audioSeconds)}, nil
}
