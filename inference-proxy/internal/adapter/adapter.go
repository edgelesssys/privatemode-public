// package adapter sets up an inference adapter for the given API type.
package adapter

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/openai"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/triton"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/unencrypted"
	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
)

const (
	// InferenceAPITriton is the name of the Triton adapter.
	InferenceAPITriton = "triton"
	// InferenceAPIOpenAI is the name of the OpenAI adapter.
	InferenceAPIOpenAI = "openai"
	// InferenceAPIUnencrypted is a meta-adapter used for testing without encryption.
	InferenceAPIUnencrypted = "unencrypted"
)

// IsSupportedInferenceAPI returns whether the given API type is supported by the Continuum.
func IsSupportedInferenceAPI(apiType string) bool {
	switch strings.ToLower(apiType) {
	case InferenceAPITriton, InferenceAPIOpenAI:
		return true
	case InferenceAPIUnencrypted:
		// Special case we always support for testing
		return true
	default:
		return false
	}
}

// New creates a new InferenceAdapter for the given API type.
func New(apiType string, cipher *cipher.Cipher, forwarder mutatingForwarder, log *slog.Logger) (InferenceAdapter, error) {
	switch strings.ToLower(apiType) {
	case InferenceAPITriton:
		return triton.New(cipher, forwarder, log)
	case InferenceAPIOpenAI:
		return openai.New(cipher, forwarder, log)
	case InferenceAPIUnencrypted:
		return unencrypted.New(forwarder, log)
	default:
		return nil, fmt.Errorf("unknown API type %q", apiType)
	}
}

// InferenceAdapter forwards requests to the inference API and handles encryption/decryption of sensitive parts.
type InferenceAdapter interface {
	ServeMux() *http.ServeMux
}

type mutatingForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMutator, forwarder.HeaderMutator)
}
