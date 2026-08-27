// Package adaptertest provides shared test stubs for inference adapter tests.
package adaptertest

import (
	"context"
	"net/http"
	"sync"

	"github.com/edgelesssys/continuum/inference-proxy/internal/secrets"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/usagecollector"
)

// Secrets is a map-backed stub for inference.SecretGetter.
type Secrets struct {
	SecretMap map[string][]byte
}

// Get returns the secret for the given ID, or an error if it is not in the map.
func (s *Secrets) Get(_ context.Context, id string) ([]byte, error) {
	secret, ok := s.SecretMap[id]
	if !ok {
		return nil, secrets.ErrSecretNotFound
	}
	return secret, nil
}

// PlainForwarder forwards a plaintext request with request/response transformation.
type PlainForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMapper, ...forwarder.Opts)
}

// EncForwarder is a stub for inference.EncForwarder. If Inner is set, requests pass through to it
// without any decryption or encryption. If not, every call is a no-op.
type EncForwarder struct {
	Inner PlainForwarder
}

// ForwardPlain forwards through Inner, or does nothing when Inner is nil.
func (f EncForwarder) ForwardPlain(
	w http.ResponseWriter,
	r *http.Request,
	mutator forwarder.RequestMutator,
	mapper forwarder.ResponseMapper,
	opts ...forwarder.Opts,
) {
	if f.Inner != nil {
		f.Inner.Forward(w, r, mutator, mapper, opts...)
	}
}

// ForwardEncrypted forwards through Inner without encryption, or does nothing when Inner is nil.
func (f EncForwarder) ForwardEncrypted(
	w http.ResponseWriter,
	r *http.Request,
	postDecryptMutator forwarder.RequestMutator,
	preEncryptMapper forwarder.ResponseMapper,
) {
	if f.Inner != nil {
		f.Inner.Forward(w, r, postDecryptMutator, preEncryptMapper)
	}
}

// UsageReporter is a stub for inference.UsageReporter that records the latest report.
type UsageReporter struct {
	mu     sync.Mutex
	latest *usagecollector.SubmitUsageReportRequest
}

// Submit records req as the latest report.
func (r *UsageReporter) Submit(_ context.Context, req usagecollector.SubmitUsageReportRequest) error {
	r.mu.Lock()
	r.latest = &req
	r.mu.Unlock()
	return nil
}

// LatestReport returns the most recently submitted report, or false if none was submitted.
func (r *UsageReporter) LatestReport() (usagecollector.SubmitUsageReportRequest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.latest == nil {
		return usagecollector.SubmitUsageReportRequest{}, false
	}
	return *r.latest, true
}
