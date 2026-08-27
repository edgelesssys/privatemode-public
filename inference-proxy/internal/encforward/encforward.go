// Package encforward provides a request forwarder that decrypts requests and encrypts responses
// for inference adapters, using the full-body encryption (FBE) scheme.
package encforward

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/edgelesssys/continuum/inference-proxy/internal/secrets"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/encbody"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
)

// Forwarder forwards requests to the workload, either as plaintext or end-to-end encrypted.
type Forwarder interface {
	// ForwardPlain forwards the request and sends back the response without any encryption.
	ForwardPlain(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMapper, ...forwarder.Opts)
	// ForwardEncrypted decrypts the downstream request, forwards it to the upstream as plaintext,
	// and encrypts the upstream's response before sending it downstream.
	ForwardEncrypted(
		w http.ResponseWriter,
		r *http.Request,
		postDecryptMutator forwarder.RequestMutator,
		preEncryptMapper forwarder.ResponseMapper,
	)
}

// secretGetter provides the inference secrets a request/response exchange is encrypted with.
type secretGetter interface {
	// Get returns the secret for the given ID, or [secrets.ErrSecretNotFound].
	Get(ctx context.Context, id string) ([]byte, error)
}

// plainForwarder forwards a plaintext request with request/response transformation.
type plainForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMapper, ...forwarder.Opts)
}

// encForwarder adds request decryption and response encryption around a plaintext
// [forwarder.Forwarder].
type encForwarder struct {
	plainForwarder plainForwarder
	secrets        secretGetter
}

// New creates a new encrypting Forwarder.
func New(plainForwarder plainForwarder, secrets secretGetter) Forwarder {
	return &encForwarder{
		plainForwarder: plainForwarder,
		secrets:        secrets,
	}
}

// ForwardPlain forwards the request and sends back the response without any encryption.
func (e *encForwarder) ForwardPlain(
	w http.ResponseWriter,
	r *http.Request,
	requestMutator forwarder.RequestMutator,
	responseMapper forwarder.ResponseMapper,
	opts ...forwarder.Opts,
) {
	e.plainForwarder.Forward(w, r, requestMutator, responseMapper, opts...)
}

// ForwardEncrypted decrypts the downstream request, forwards it to the upstream as plaintext, and
// encrypts the upstream's response before sending it downstream.
//
// postDecryptMutator modifies the downstream (plaintext) request after decryption. preEncryptMapper
// maps the upstream (plaintext) response and the mapped response is encrypted.
func (e *encForwarder) ForwardEncrypted(
	w http.ResponseWriter,
	r *http.Request,
	postDecryptMutator forwarder.RequestMutator,
	preEncryptMapper forwarder.ResponseMapper,
) {
	// Set by every successful execution of the request mutator.
	var exchange *encbody.ServerExchange

	if !encbody.IsEncrypted(r.Header) {
		forwarder.HTTPError(w, r, http.StatusBadRequest, "request must be encrypted")
		return
	}
	secretID := r.Header.Get(constants.PrivatemodeSecretIDHeader)
	if secretID == "" {
		forwarder.HTTPError(w, r, http.StatusBadRequest, "request must contain secret ID")
		return
	}
	secret, err := e.secrets.Get(r.Context(), secretID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, secrets.ErrSecretNotFound) {
			// [constants.ErrorNoSecretForID] is matched by clients
			err = fmt.Errorf("%s %q: %w", constants.ErrorNoSecretForID, secretID, err)
			status = http.StatusBadRequest
		}
		forwarder.HTTPError(w, r, status, "%v", err)
		return
	}

	e.plainForwarder.Forward(
		w, r,
		func(r *http.Request) error {
			var err error
			exchange, err = encbody.InitializeServerExchange(secret, r)
			if err != nil {
				return fmt.Errorf("initializing server exchange: %w", err)
			}
			if err := exchange.DecryptRequest(r); err != nil {
				return fmt.Errorf("decrypting request: %w", err)
			}
			if err := postDecryptMutator(r); err != nil {
				return fmt.Errorf("mutating request post-decryption: %w", err)
			}
			return nil
		},
		func(resp *http.Response) (forwarder.Response, error) {
			dsResp, err := preEncryptMapper(resp)
			if err != nil {
				return nil, fmt.Errorf("mapping response pre-encryption: %w", err)
			}
			return encrypt(dsResp, exchange)
		},
	)
}

func encrypt(resp forwarder.Response, exchange *encbody.ServerExchange) (forwarder.Response, error) {
	switch r := resp.(type) {
	case *forwarder.UnaryResponse:
		if err := exchange.EncryptUnaryResponse(r); err != nil {
			return nil, fmt.Errorf("encrypting unary response: %w", err)
		}
		return r, nil
	case *forwarder.StreamingResponse:
		if err := exchange.EncryptSSEResponse(r, constants.MaxSSEEventBytes); err != nil {
			return nil, fmt.Errorf("encrypting SSE response: %w", err)
		}
		return r, nil
	default:
		return nil, fmt.Errorf("unexpected response type %T", resp)
	}
}
