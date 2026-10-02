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
	"github.com/edgelesssys/continuum/inference-proxy/internal/encforward"
	"github.com/edgelesssys/continuum/internal/oss/compat"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/model"
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
	mux.HandleFunc("GET "+constants.OpenAIModelsEndpoint, a.forwardModelsRequest)
	mux.HandleFunc("GET "+constants.OpenAIModelsEndpoint+"/{model}", a.forwardSpecificModelRequest) // Also handle model details endpoint

	// Create chat completion: https://platform.openai.com/docs/api-reference/chat/create
	mux.Handle(constants.OpenAIChatCompletionsEndpoint, a.VerifyOCSP(http.HandlerFunc(a.forwardChatCompletionsRequest)))

	// Legacy chat completions endpoint: https://platform.openai.com/docs/api-reference/completions/create
	// Reuse the same handler as for /v1/chat/completions since both endpoints require the same mutators.
	mux.Handle(constants.OpenAILegacyCompletionsEndpoint, a.VerifyOCSP(http.HandlerFunc(a.forwardChatCompletionsRequest)))

	// Create embeddings: https://platform.openai.com/docs/api-reference/embeddings/create
	mux.Handle("POST "+constants.OpenAIEmbeddingsEndpoint, a.VerifyOCSP(http.HandlerFunc(a.forwardEmbeddingsRequest)))

	mux.Handle(constants.OpenAITranscriptionsEndpoint, a.VerifyOCSP(http.HandlerFunc(a.forwardTranscriptionsRequest)))
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
	a.ForwardEncrypted(
		w, r,
		forwarder.NoRequestMutation,
		nil,
		nil,
		extractOpenAIUsage,
		extractOpenAIUsage,
	)
}

func (a *Adapter) forwardTranscriptionsRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a.ForwardEncrypted(
		w, r,
		a.mutators.AudioStreamUsageReportingInjector,
		legacyStringDurationMutation(r.Header.Get(constants.PrivatemodeVersionHeader)),
		nil,
		extractUnaryTranscriptionUsage,
		a.extractStreamingTranscriptionUsage(ctx),
	)
}

// legacyStringDurationMutation returns a mutation that re-encodes the top-level `duration` of a
// verbose_json transcription response as a JSON string, or nil for clients that expect the number
// the OpenAI API specifies.
//
// vLLM encoded the field as a string up to v0.24.0 and as a number since v0.27.1, so the upgrade
// would change our API's response type. Keep the string for clients up to v1.54, which is the
// release that announces the change.
// TODO: drop this once the minimum client version is past v1.54.
func legacyStringDurationMutation(clientVersion string) inference.MutationFunc {
	if compat.AtLeastMajorMinor(clientVersion, 1, 55) {
		return nil
	}

	return func(data []byte) ([]byte, error) {
		if len(data) == 0 || !gjson.ValidBytes(data) {
			return data, nil
		}
		duration := gjson.GetBytes(data, "duration")
		if duration.Type != gjson.Number {
			return data, nil
		}
		// Setting a Go string writes a JSON string. Use the raw value to keep the digits vLLM sent.
		return sjson.SetBytes(data, "duration", duration.Raw)
	}
}

// forwardChatCompletionsRequest forwards a request to the chat completions endpoint.
func (a *Adapter) forwardChatCompletionsRequest(w http.ResponseWriter, r *http.Request) {
	a.ForwardEncrypted(
		w, r,
		forwarder.RequestMutatorChain(
			a.mutators.CacheSaltValidator,
			a.mutators.MediaContentValidator,
			a.mutators.StreamUsageReportingInjector,
		),
		// TODO: remove once clients have caught up.
		//       We already deprecated the field and announced removal after 1st of May 26, but
		//       OpenCode does not yet support the "reasoning" field. Tracked in PM-158
		duplicateReasoningFieldInJSON,
		duplicateReasoningFieldInJSON,
		extractOpenAIUsage,
		extractOpenAIUsage,
	)
}

// duplicateReasoningFieldInJSON copies the "reasoning" field of response messages into
// a "reasoning_content" field alongside it, for backwards compatibility.
func duplicateReasoningFieldInJSON(data []byte) ([]byte, error) {
	if len(data) == 0 || !gjson.ValidBytes(data) {
		return data, nil
	}

	choices := gjson.GetBytes(data, "choices")
	if !choices.IsArray() {
		return data, nil
	}

	var mutateErr error
	choices.ForEach(func(key, _ gjson.Result) bool {
		for _, field := range []string{"message", "delta"} {
			reasoningPath := "choices." + key.String() + "." + field + ".reasoning"
			reasoning := gjson.GetBytes(data, reasoningPath)
			if !reasoning.Exists() {
				continue
			}

			data, mutateErr = sjson.SetRawBytes(data, "choices."+key.String()+"."+field+".reasoning_content", []byte(reasoning.Raw))
			if mutateErr != nil {
				return false
			}
		}
		return true
	})
	if mutateErr != nil {
		return nil, mutateErr
	}

	return data, nil
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
