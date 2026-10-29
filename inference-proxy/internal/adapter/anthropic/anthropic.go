/*
Package anthropic implements an inference API adapter for the [Anthropic Messages API].

[Anthropic Messages API]: https://docs.anthropic.com/en/api/messages
*/
package anthropic

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/inference"
	"github.com/edgelesssys/continuum/inference-proxy/internal/encforward"
	"github.com/edgelesssys/continuum/internal/oss/anthropic"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/usage"
)

// Adapter implements an inference adapter for the Anthropic API.
type Adapter struct {
	*inference.Adapter
	cacheSaltValidator    forwarder.RequestMutator
	legacyCacheSaltHasher forwarder.RequestMutator
	mediaContentValidator forwarder.RequestMutator
}

// New creates a new [Adapter] for the Anthropic API.
func New(workloadTasks []string, secrets inference.SecretGetter, ocspStatusFile string, encForwarder encforward.Forwarder, reporter inference.UsageReporter, log *slog.Logger) (*Adapter, error) {
	baseAdapter, err := inference.New(workloadTasks, secrets, ocspStatusFile, encForwarder, reporter, log)
	if err != nil {
		return nil, err
	}

	return &Adapter{
		Adapter:               baseAdapter,
		cacheSaltValidator:    openai.CacheSaltValidator(log),
		legacyCacheSaltHasher: openai.LegacyCacheSaltHasher(log),
		mediaContentValidator: anthropic.MediaContentValidator(log),
	}, nil
}

// RegisterRoutes registers the Anthropic API handlers on the given ServeMux.
func (a *Adapter) RegisterRoutes(mux *http.ServeMux) {
	// Create message: https://docs.anthropic.com/en/api/messages
	mux.HandleFunc("POST "+constants.AnthropicMessagesEndpoint, a.forwardMessagesRequest)
}

// HandlesCatchAll returns false because Anthropic adapter only handles specific endpoints.
func (a *Adapter) HandlesCatchAll() bool {
	return false
}

// forwardMessagesRequest forwards a request to the Anthropic messages endpoint.
func (a *Adapter) forwardMessagesRequest(w http.ResponseWriter, r *http.Request) {
	a.ForwardEncrypted(
		w, r,
		forwarder.RequestMutatorChain(inference.RejectAnonymousRequest, a.cacheSaltValidator, a.legacyCacheSaltHasher, a.mediaContentValidator),
		nil,
		nil,
		extractAnthropicUsageUnary,
		extractAnthropicUsageStream(),
	)
}

func extractAnthropicUsageUnary(body []byte) (usage.Stats, error) {
	var parsed struct {
		Usage *anthropic.Usage `json:"usage"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return usage.Stats{}, err
	}
	if parsed.Usage == nil {
		return usage.Stats{}, inference.ErrNoUsage
	}
	return parsed.Usage.ToUsageStats(), nil
}

// extractAnthropicUsageStream creates an extractor with usage state for a single request.
// Each event contains cumulative counts, but may omit fields that have been sent previously (e.g. input_tokens).
func extractAnthropicUsageStream() inference.UsageExtractor {
	var latest anthropic.Usage
	return func(body []byte) (usage.Stats, error) {
		var parsed struct {
			Type    string           `json:"type"`
			Usage   *anthropic.Usage `json:"usage,omitzero"`
			Message struct {
				Usage *anthropic.Usage `json:"usage,omitzero"`
			} `json:"message"`
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
		}
		err := json.Unmarshal(body, &parsed)
		if err != nil {
			return usage.Stats{}, err
		}

		var next *anthropic.Usage
		switch parsed.Type {
		case "message_start":
			next = parsed.Message.Usage
		case "message_delta":
			next = parsed.Usage
			// If this was the final event in the stream, simply return
			// the usage in that message.
			// This is important since the final message splits up input tokens
			// into regular input, cached creation, and cache read tokens.
			// Simply adding this onto existing information in 'latest' results
			// in over calculation for those values.
			if next != nil && parsed.Delta.StopReason != "" {
				return next.ToUsageStats(), nil
			}
		}
		if next == nil {
			return usage.Stats{}, inference.ErrNoUsage
		}

		latest = anthropic.Usage{
			InputTokens:              max(next.InputTokens, latest.InputTokens),
			OutputTokens:             max(next.OutputTokens, latest.OutputTokens),
			CacheReadInputTokens:     max(next.CacheReadInputTokens, latest.CacheReadInputTokens),
			CacheCreationInputTokens: max(next.CacheCreationInputTokens, latest.CacheCreationInputTokens),
		}
		return latest.ToUsageStats(), nil
	}
}
