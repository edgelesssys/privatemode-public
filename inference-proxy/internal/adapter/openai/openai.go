/*
Package openai implements an inference API adapter for the [OpenAI API spec].

[OpenAI API sepc]: https://platform.openai.com/docs/api-reference
*/
package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/ocsp"
	"github.com/edgelesssys/continuum/internal/oss/ocspheader"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var ocspStatusMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "privatemode_nvidia_ocsp_status",
	Help: "NVIDIA OCSP status of the attested components (0=good, 1=revoked, -1=unknown)",
}, []string{"i", "component"})

// Adapter implements an InferenceAdapter for OpenAI API.
type Adapter struct {
	cipher        responseCipherCreator
	forwarder     mutatingForwarder
	workloadTasks []string
	mutators      openai.DefaultRequestMutators
	log           *slog.Logger
	ocspStatus    []ocsp.StatusInfo
}

// responseCipherCreator is the interface used for creating new ciphers for response encryption.
type responseCipherCreator interface {
	// NewResponseCipher creates a new [cipher.ResponseCipher] for encrypting responses.
	NewResponseCipher() cipher.ResponseCipher
	Secret(ctx context.Context, id string) ([]byte, error)
}

// New creates a new InferenceAdapter for the OpenAI API.
func New(workloadTasks []string, cipher responseCipherCreator, ocspStatusFile string, forwarder mutatingForwarder, log *slog.Logger) (*Adapter, error) {
	if len(workloadTasks) == 0 {
		return nil, errors.New("no workload tasks provided")
	}

	ocspStatusJSON, err := os.ReadFile(ocspStatusFile)
	if err != nil {
		return nil, fmt.Errorf("reading OCSP status file: %w", err)
	}
	var ocspStatus []ocsp.StatusInfo
	if err := json.Unmarshal(ocspStatusJSON, &ocspStatus); err != nil {
		return nil, fmt.Errorf("unmarshalling OCSP status JSON: %w", err)
	}

	for i, statusInfo := range ocspStatus {
		addOCSPStatusMetric(i, "gpu", statusInfo.GPU)
		addOCSPStatusMetric(i, "driver", statusInfo.Driver)
		addOCSPStatusMetric(i, "vbios", statusInfo.VBIOS)
	}

	return &Adapter{
		cipher:        cipher,
		forwarder:     forwarder,
		workloadTasks: workloadTasks,
		ocspStatus:    ocspStatus,
		mutators:      openai.GetDefaultRequestMutators(openai.RandomPromptCacheSalt, log),
		log:           log,
	}, nil
}

// ServeMux returns a multiplexer for intercepting OpenAI API requests.
func (t *Adapter) ServeMux() http.Handler {
	srv := http.NewServeMux()

	// Reject traffic to unsupported endpoints
	srv.HandleFunc("/", t.unsupportedEndpoint)

	// List models: https://platform.openai.com/docs/api-reference/models/list
	srv.HandleFunc("GET /v1/models", t.forwardModelsRequest)

	// Create chat completion: https://platform.openai.com/docs/api-reference/chat/create
	srv.HandleFunc("/v1/chat/completions", t.forwardChatCompletionsRequest)

	// Legacy chat completions endpoint: https://platform.openai.com/docs/api-reference/completions/create
	// Reuse the same handler as for /v1/chat/completions since the unencrypted fields are the same.
	srv.HandleFunc(openai.LegacyCompletionsEndpoint, t.forwardChatCompletionsRequest)

	// Create embeddings: https://platform.openai.com/docs/api-reference/embeddings/create
	srv.HandleFunc("POST /v1/embeddings", t.forwardEmbeddingsRequest)

	srv.HandleFunc(openai.TranscriptionsEndpoint, t.forwardTranscriptionsRequest)

	srv.HandleFunc(openai.TranslationsEndpoint, t.forwardTranslationsRequest)

	return t.verifyOCSP(srv)
}

func (t *Adapter) verifyOCSP(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == openai.ModelsEndpoint { // Models endpoint is excluded from GPU attestation
			h.ServeHTTP(w, r)
			return
		}

		ocspPolicy := r.Header.Get(constants.PrivatemodeNvidiaOCSPPolicyHeader)
		ocspMAC := r.Header.Get(constants.PrivatemodeNvidiaOCSPPolicyMACHeader)
		secretID := r.Header.Get(constants.PrivatemodeSecretIDHeader)

		var acceptedStatuses []ocsp.Status
		if ocspPolicy == "" && ocspMAC == "" {
			acceptedStatuses = []ocsp.Status{ocsp.StatusGood} // Old clients won't set the header, only accept good status
		} else {
			secret, err := t.cipher.Secret(r.Context(), secretID)
			if err != nil {
				forwarder.HTTPError(w, r, http.StatusInternalServerError, "getting secret for OCSP verification: %s", err)
				return
			}
			if len(secret) != 32 {
				forwarder.HTTPError(w, r, http.StatusInternalServerError, "invalid secret length for OCSP verification: expected 32 bytes, got %d", len(secret))
				return
			}

			requestedOCSPStatus, err := ocspheader.UnmarshalAndVerify(ocspPolicy, ocspMAC, [32]byte(secret))
			if err != nil {
				forwarder.HTTPError(w, r, http.StatusInternalServerError, "verifying OCSP header: %s", err)
				return
			}

			for _, allowedStatus := range requestedOCSPStatus.AllowedStatuses {
				switch allowedStatus {
				case ocspheader.AllowStatusGood:
					acceptedStatuses = append(acceptedStatuses, ocsp.StatusGood)
				case ocspheader.AllowStatusUnknown:
					acceptedStatuses = append(acceptedStatuses, ocsp.StatusUnknown)
				case ocspheader.AllowStatusRevoked:
					acceptedStatuses = append(acceptedStatuses, ocsp.StatusRevoked(requestedOCSPStatus.RevokedNbf))
				}
			}
		}

		for _, status := range t.ocspStatus {
			if !status.Driver.AcceptedBy(acceptedStatuses) {
				forwarder.HTTPError(w, r, http.StatusInternalServerError, "GPU attestation returned a driver OCSP status that is not accepted by the client: %s", status.Driver)
				return
			}
			if !status.GPU.AcceptedBy(acceptedStatuses) {
				forwarder.HTTPError(w, r, http.StatusInternalServerError, "GPU attestation returned a GPU OCSP status that is not accepted by the client: %s", status.GPU)
				return
			}
			if !status.VBIOS.AcceptedBy(acceptedStatuses) {
				forwarder.HTTPError(w, r, http.StatusInternalServerError, "GPU attestation returned a VBIOS OCSP status that is not accepted by the client: %s", status.VBIOS)
				return
			}
		}

		h.ServeHTTP(w, r)
	})
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
func (t *Adapter) forwardChatCompletionsRequest(w http.ResponseWriter, r *http.Request) {
	session := t.cipher.NewResponseCipher()
	t.forwarder.Forward(
		w, r,
		forwarder.RequestMutatorChain(
			forwarder.WithFullJSONRequestMutation(session.DecryptRequest(r.Context()), openai.PlainCompletionsRequestFields, t.log),
			t.mutators.CacheSaltValidator,
			t.mutators.SecureImageURLValidator,
		),
		forwarder.WithFullJSONResponseMutation(session.EncryptResponse(r.Context()), openai.PlainCompletionsResponseFields, false),
		forwarder.NoHeaderMutation,
	)
}

type mutatingForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMutator, forwarder.HeaderMutator, ...forwarder.Opts)
}

func addOCSPStatusMetric(index int, component string, status ocsp.Status) {
	var statusFloat float64
	switch status.Value {
	case ocsp.StatusGood.Value:
		statusFloat = 0
	case ocsp.StatusRevoked(time.Time{}).Value:
		statusFloat = 1
	case ocsp.StatusUnknown.Value:
		statusFloat = -1
	}
	ocspStatusMetrics.WithLabelValues(fmt.Sprintf("gpu_index_%d", index), component).Set(statusFloat)
}
