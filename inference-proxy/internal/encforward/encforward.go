// Package encforward provides a request forwarder that decrypts requests and encrypts responses
// for inference adapters.
//
// Two protocol variants are supported, selected by the client version header: the full-body
// encryption (FBE) scheme and the legacy field-based scheme.
package encforward

import (
	"context"
	"fmt"
	"net/http"

	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/oss/compat"
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
		legacyDecryptMutator func(cipher.ResponseCipher) forwarder.RequestMutator,
		postDecryptMutator forwarder.RequestMutator,
		legacyPlainResponseFields forwarder.FieldSelector,
		preEncryptMapper forwarder.ResponseMapper,
	)
}

// responseCipherCreator creates cipher instances for legacy encryption and provides direct access
// to secrets for FBE.
//
// When the legacy scheme is removed, this should be replaced by a secretsGetter interface. Most
// importantly, this [Forwarder] must take on the responsibility of putting
// [constants.ErrorNoSecretForID] into the response message. That is required so that clients know
// to upload a fresh secret.
type responseCipherCreator interface {
	// NewResponseCipher creates a new [cipher.ResponseCipher] for the legacy scheme.
	NewResponseCipher() cipher.ResponseCipher
	// Secret returns the secret for the given ID.
	Secret(ctx context.Context, id string) ([]byte, error)
}

// plainForwarder forwards a plaintext request with request/response transformation.
type plainForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMapper, ...forwarder.Opts)
}

// encForwarder adds request decryption and response encryption around a plaintext
// [forwarder.Forwarder].
type encForwarder struct {
	plainForwarder plainForwarder
	cipher         responseCipherCreator
}

// New creates a new encrypting Forwarder.
func New(plainForwarder plainForwarder, cipher responseCipherCreator) Forwarder {
	return &encForwarder{
		plainForwarder: plainForwarder,
		cipher:         cipher,
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
	legacyDecryptMutator func(cipher.ResponseCipher) forwarder.RequestMutator,
	postDecryptMutator forwarder.RequestMutator,
	legacyPlainResponseFields forwarder.FieldSelector,
	preEncryptMapper forwarder.ResponseMapper,
) {
	if compat.AtLeastMajorMinor(r.Header.Get(constants.PrivatemodeVersionHeader), 1, 46) {
		e.forwardEncryptedFBE(w, r, postDecryptMutator, preEncryptMapper)
	} else {
		e.forwardEncryptedLegacy(w, r, legacyDecryptMutator, postDecryptMutator, legacyPlainResponseFields, preEncryptMapper)
	}
}

func (e *encForwarder) forwardEncryptedLegacy(
	w http.ResponseWriter,
	r *http.Request,
	legacyDecryptMutator func(cipher.ResponseCipher) forwarder.RequestMutator,
	postDecryptMutator forwarder.RequestMutator,
	legacyPlainResponseFields forwarder.FieldSelector,
	preEncryptMapper forwarder.ResponseMapper,
) {
	session := e.cipher.NewResponseCipher()
	encryptMutator := forwarder.NewJSONMutatingReader(session.EncryptResponse(r.Context()), legacyPlainResponseFields)

	e.plainForwarder.Forward(
		w, r,
		forwarder.RequestMutatorChain(legacyDecryptMutator(session), postDecryptMutator),
		func(resp *http.Response) (forwarder.Response, error) {
			dsResp, err := preEncryptMapper(resp)
			if err != nil {
				return nil, err
			}
			return encryptLegacy(dsResp, encryptMutator)
		},
	)
}

func encryptLegacy(resp forwarder.Response, encryptMutator *forwarder.MutatingReader) (forwarder.Response, error) {
	switch r := resp.(type) {
	case *forwarder.UnaryResponse:
		body, err := encryptMutator.Mutate(r.Body)
		if err != nil {
			return nil, fmt.Errorf("encrypting unary response: %w", err)
		}
		r.Body = body
		return r, nil
	case *forwarder.StreamingResponse:
		r.Body = encryptMutator.Reader(r.Body)
		return r, nil
	default:
		return nil, fmt.Errorf("unexpected response type %T", resp)
	}
}

func (e *encForwarder) forwardEncryptedFBE(
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
	secret, err := e.cipher.Secret(r.Context(), secretID)
	if err != nil {
		forwarder.HTTPError(w, r, http.StatusBadRequest, "%v", err)
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
			return encryptFBE(dsResp, exchange)
		},
	)
}

func encryptFBE(resp forwarder.Response, exchange *encbody.ServerExchange) (forwarder.Response, error) {
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
