// Package adapter sets up an inference adapter for the given API type.
package adapter

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/openai"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/unencrypted"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/unstructured"
	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
)

const (
	// InferenceAPIOpenAI is the name of the OpenAI adapter.
	InferenceAPIOpenAI = "openai"
	// InferenceAPIUnencrypted is a meta-adapter used for testing without encryption.
	InferenceAPIUnencrypted = "unencrypted"
	// InferenceAPIUnstructured is an adapter for the Unstructured API.
	InferenceAPIUnstructured = "unstructured"
)

// IsSupportedInferenceAPI returns whether the given API type is supported by the Continuum.
func IsSupportedInferenceAPI(apiType string) bool {
	switch strings.ToLower(apiType) {
	case InferenceAPIOpenAI, InferenceAPIUnstructured:
		return true
	case InferenceAPIUnencrypted:
		// Special case we always support for testing
		return true
	default:
		return false
	}
}

// New creates a new InferenceAdapter for the given API type.
func New(
	apiType string, workloadTasks []string, cipher *cipher.Cipher, ocspStatusFile string, forwarder mutatingForwarder, log *slog.Logger,
) (InferenceAdapter, error) {
	switch strings.ToLower(apiType) {
	case InferenceAPIOpenAI:
		return openai.New(workloadTasks, cipher, ocspStatusFile, forwarder, log)
	case InferenceAPIUnstructured:
		return unstructured.New(cipher, forwarder, log)
	case InferenceAPIUnencrypted:
		return unencrypted.New(forwarder, log)
	default:
		return nil, fmt.Errorf("unknown API type %q", apiType)
	}
}

// InferenceAdapter forwards requests to the inference API and handles encryption/decryption of sensitive parts.
type InferenceAdapter interface {
	ServeMux() http.Handler
}

type mutatingForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMutator, forwarder.HeaderMutator, ...forwarder.Opts)
}
