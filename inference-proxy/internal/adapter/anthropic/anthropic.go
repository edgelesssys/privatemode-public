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
	"github.com/edgelesssys/continuum/internal/oss/anthropic"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/usage"
)

// Adapter implements an inference adapter for the Anthropic API.
type Adapter struct {
	*inference.Adapter
	cacheSaltValidator    forwarder.RequestMutator
	mediaContentValidator forwarder.RequestMutator
}

// New creates a new [Adapter] for the Anthropic API.
func New(workloadTasks []string, cipher inference.ResponseCipherCreator, ocspStatusFile string, forwarder inference.MutatingForwarder, log *slog.Logger) (*Adapter, error) {
	// No endpoints are excluded from OCSP verification for Anthropic
	baseAdapter, err := inference.New(workloadTasks, cipher, ocspStatusFile, forwarder, log)
	if err != nil {
		return nil, err
	}

	return &Adapter{
		Adapter:               baseAdapter,
		cacheSaltValidator:    openai.CacheSaltValidator(log),
		mediaContentValidator: anthropic.MediaContentValidator(log),
	}, nil
}

// RegisterRoutes registers the Anthropic API handlers on the given ServeMux.
// Each handler is wrapped with OCSP verification middleware.
func (a *Adapter) RegisterRoutes(mux *http.ServeMux) {
	// Create message: https://docs.anthropic.com/en/api/messages
	mux.Handle("POST "+anthropic.MessagesEndpoint, a.VerifyOCSP(http.HandlerFunc(a.forwardMessagesRequest)))
}

// HandlesCatchAll returns false because Anthropic adapter only handles specific endpoints.
func (a *Adapter) HandlesCatchAll() bool {
	return false
}

// forwardMessagesRequest forwards a request to the Anthropic messages endpoint.
func (a *Adapter) forwardMessagesRequest(w http.ResponseWriter, r *http.Request) {
	session := a.Cipher.NewResponseCipher()
	encryptMutator := forwarder.NewJSONMutatingReader(session.EncryptResponse(r.Context()), anthropic.PlainMessagesResponseFields)

	a.Forwarder.Forward(
		w, r,
		forwarder.RequestMutatorChain(
			forwarder.WithJSONRequestMutation(session.DecryptRequest(r.Context()), anthropic.PlainMessagesRequestFields, a.Log),
			a.cacheSaltValidator,
			a.mediaContentValidator,
		),
		a.ResponseMapper(encryptMutator, extractAnthropicUsage, extractAnthropicUsage),
	)
}

func extractAnthropicUsage(body []byte) (usage.Stats, error) {
	// In streaming responses, usage information is only included in message_delta events, while
	// response tokens are sent in content_block_delta events. We attempt simply attempt parsing
	// the known usage structure.
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
