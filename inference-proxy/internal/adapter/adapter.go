// Package adapter sets up an inference adapter for the given API type.
package adapter

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/anthropic"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/openai"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/unencrypted"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/unstructured"
	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
)

const (
	// InferenceAPIOpenAI is the name of the OpenAI adapter.
	InferenceAPIOpenAI = "openai"
	// InferenceAPIAnthropic is the name of the Anthropic adapter.
	InferenceAPIAnthropic = "anthropic"
	// InferenceAPIUnencrypted is a meta-adapter used for testing without encryption.
	InferenceAPIUnencrypted = "unencrypted"
	// InferenceAPIUnstructured is an adapter for the Unstructured API.
	InferenceAPIUnstructured = "unstructured"
)

// IsSupportedInferenceAPI returns whether the given API type is supported by the Continuum.
func IsSupportedInferenceAPI(apiType string) bool {
	switch strings.ToLower(apiType) {
	case InferenceAPIOpenAI, InferenceAPIUnstructured, InferenceAPIAnthropic:
		return true
	case InferenceAPIUnencrypted:
		// Special case we always support for testing
		return true
	default:
		return false
	}
}

// New creates InferenceAdapters for the given API types.
func New(
	apiTypes []string, workloadTasks []string, cipher *cipher.Cipher, ocspStatusFile string, forwarder mutatingForwarder, log *slog.Logger,
) ([]InferenceAdapter, error) {
	var adapters []InferenceAdapter
	for _, apiType := range apiTypes {
		var adapter InferenceAdapter
		var err error
		switch strings.ToLower(apiType) {
		case InferenceAPIOpenAI:
			adapter, err = openai.New(workloadTasks, cipher, ocspStatusFile, forwarder, log)
		case InferenceAPIAnthropic:
			adapter, err = anthropic.New(workloadTasks, cipher, ocspStatusFile, forwarder, log)
		case InferenceAPIUnstructured:
			adapter, err = unstructured.New(cipher, forwarder, log)
		case InferenceAPIUnencrypted:
			adapter, err = unencrypted.New(forwarder, log)
		default:
			return nil, fmt.Errorf("unknown API type %q", apiType)
		}
		if err != nil {
			return nil, fmt.Errorf("creating adapter for %q: %w", apiType, err)
		}
		adapters = append(adapters, adapter)
	}
	return adapters, nil
}

// InferenceAdapter forwards requests to the inference API and handles encryption/decryption of sensitive parts.
type InferenceAdapter interface {
	// RegisterRoutes registers the adapter's HTTP handlers on the given ServeMux.
	// Handlers should already have any necessary middleware (e.g., OCSP verification) applied.
	RegisterRoutes(mux *http.ServeMux)
	// HandlesCatchAll returns true if this adapter registers a catch-all "/" handler.
	// If any adapter handles catch-all, the server won't register its own 501 handler.
	HandlesCatchAll() bool
}

type mutatingForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMutator, forwarder.HeaderMutator, ...forwarder.Opts)
}

// UnsupportedEndpoint returns 501 Not Implemented.
// To be used as the default handler for endpoints not supported by any adapter.
func UnsupportedEndpoint(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "unsupported endpoint", http.StatusNotImplemented)
}
