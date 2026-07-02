/*
Package openai implements an inference API adapter for the [OpenAI API spec].

[OpenAI API sepc]: https://platform.openai.com/docs/api-reference
*/
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/inference"
	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/inference-proxy/internal/encforward"
	"github.com/edgelesssys/continuum/internal/model"
	"github.com/edgelesssys/continuum/internal/oss/constants"
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
func New(workloadTasks []string, secrets inference.SecretGetter, ocspStatusFile string, encForwarder encforward.Forwarder, reporter inference.UsageReporter, log *slog.Logger) (*Adapter, error) {
	baseAdapter, err := inference.New(workloadTasks, secrets, ocspStatusFile, encForwarder, reporter, log)
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
	mutate := func(body []byte) ([]byte, error) {
		data := gjson.GetBytes(body, "data")
		if !data.Exists() || !data.IsArray() {
			return body, nil
		}

		var mutateErr error
		data.ForEach(func(key, value gjson.Result) bool {
			if !value.IsObject() {
				return true
			}
			path := "data." + key.String() + ".tasks"
			body, mutateErr = sjson.SetBytes(body, path, a.WorkloadTasks)
			return mutateErr == nil // continue if no error
		})
		if mutateErr != nil {
			return nil, fmt.Errorf("injecting tasks into model list: %w", mutateErr)
		}
		return body, nil
	}

	a.EncForwarder.ForwardPlain(
		w, r,
		forwarder.NoRequestMutation,
		mapSuccessResponseBody(mutate),
	)
}

func (a *Adapter) forwardSpecificModelRequest(w http.ResponseWriter, r *http.Request) {
	mutate := func(body []byte) ([]byte, error) {
		body, err := sjson.SetBytes(body, "tasks", a.WorkloadTasks)
		if err != nil {
			return nil, fmt.Errorf("injecting tasks into model: %w", err)
		}
		return body, nil
	}

	a.EncForwarder.ForwardPlain(
		w, r,
		forwarder.NoRequestMutation,
		mapSuccessResponseBody(mutate),
	)
}

func mapSuccessResponseBody(fn func([]byte) ([]byte, error)) forwarder.ResponseMapper {
	return func(resp *http.Response) (forwarder.Response, error) {
		unaryResp, err := forwarder.ReadUnaryResponse(resp, constants.MaxUnaryResponseBodyBytes)
		if err != nil {
			return nil, fmt.Errorf("reading upstream response body: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return unaryResp, nil
		}
		unaryResp.Body, err = fn(unaryResp.Body)
		if err != nil {
			return nil, err
		}
		return unaryResp, nil
	}
}

func (a *Adapter) forwardEmbeddingsRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a.ForwardEncrypted(
		w, r,
		func(s cipher.ResponseCipher) forwarder.RequestMutator {
			return forwarder.WithJSONRequestMutation(s.DecryptRequest(ctx), openai.PlainEmbeddingsRequestFields, a.Log)
		},
		forwarder.NoRequestMutation,
		openai.PlainEmbeddingsResponseFields,
		nil,
		extractOpenAIUsage,
		extractOpenAIUsage,
	)
}

func (a *Adapter) forwardTranscriptionsRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a.ForwardEncrypted(
		w, r,
		func(s cipher.ResponseCipher) forwarder.RequestMutator {
			return forwarder.WithFormRequestMutation(s.DecryptRequest(ctx), openai.PlainTranscriptionRequestFields, a.Log)
		},
		a.mutators.AudioStreamUsageReportingInjector,
		openai.PlainTranscriptionResponseFields,
		nil,
		extractUnaryTranscriptionUsage,
		a.extractStreamingTranscriptionUsage(ctx),
	)
}

// forwardChatCompletionsRequest forwards chat completions with field mutation using the given selectors.
func (a *Adapter) forwardChatCompletionsRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a.ForwardEncrypted(
		w, r,
		func(s cipher.ResponseCipher) forwarder.RequestMutator {
			return forwarder.WithJSONRequestMutation(s.DecryptRequest(ctx), openai.PlainCompletionsRequestFields, a.Log)
		},
		forwarder.RequestMutatorChain(
			a.mutators.CacheSaltValidator,
			a.mutators.MediaContentValidator,
			a.mutators.StreamUsageReportingInjector,
		),
		openai.PlainCompletionsResponseFields,
		// TODO: remove after May 1st
		forwarder.NewJSONMutatingReader(duplicateReasoningFieldInJSON, forwarder.FieldSelector{}),
		extractOpenAIUsage,
		extractOpenAIUsage,
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

func (a *Adapter) extractStreamingTranscriptionUsage(ctx context.Context) func([]byte) (usage.Stats, error) {
	return func(body []byte) (usage.Stats, error) {
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
			a.Log.WarnContext(
				ctx,
				"Unknown transcription model, falling back to Voxtral billing formula",
				"model", parsed.Model,
			)
			audioSeconds = (baseUsage.PromptTokens - model.TokenOverheadVoxtral) / model.TokensPerAudioSecondVoxtral
		}

		return usage.Stats{AudioSeconds: max(0, audioSeconds)}, nil
	}
}
