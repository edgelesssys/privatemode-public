// Package inference provides shared functionality for inference API adapters.
package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/edgelesssys/continuum/internal/oss/sse"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const maxSSELineBytes = 1024 * 1024 // 1 MiB

var ocspStatusMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "privatemode_nvidia_ocsp_status",
	Help: "NVIDIA OCSP status of the attested components (0=good, 1=revoked, -1=unknown)",
}, []string{"i", "component"})

// ResponseCipherCreator is the interface used for creating new ciphers for response encryption.
type ResponseCipherCreator interface {
	// NewResponseCipher creates a new [cipher.ResponseCipher] for encrypting responses.
	NewResponseCipher() cipher.ResponseCipher
	Secret(ctx context.Context, id string) ([]byte, error)
}

// MutatingForwarder forwards requests with mutation support.
type MutatingForwarder interface {
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMapper, ...forwarder.Opts)
}

// Adapter contains common functionality shared by all inference API adapters.
type Adapter struct {
	Cipher        ResponseCipherCreator
	Forwarder     MutatingForwarder
	WorkloadTasks []string
	OCSPStatus    []ocsp.StatusInfo
	Log           *slog.Logger
}

// New creates a new base Adapter with common functionality.
func New(workloadTasks []string, cipher ResponseCipherCreator, ocspStatusFile string,
	forwarder MutatingForwarder, log *slog.Logger,
) (*Adapter, error) {
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
		Cipher:        cipher,
		Forwarder:     forwarder,
		WorkloadTasks: workloadTasks,
		OCSPStatus:    ocspStatus,
		Log:           log,
	}, nil
}

// VerifyOCSP returns OCSP verification middleware that wraps the given handler.
// This should be applied per-route by adapters that require OCSP verification.
func (a *Adapter) VerifyOCSP(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ocspPolicy := r.Header.Get(constants.PrivatemodeNvidiaOCSPPolicyHeader)
		ocspMAC := r.Header.Get(constants.PrivatemodeNvidiaOCSPPolicyMACHeader)
		secretID := r.Header.Get(constants.PrivatemodeSecretIDHeader)

		var acceptedStatuses []ocsp.Status
		if ocspPolicy == "" && ocspMAC == "" {
			acceptedStatuses = []ocsp.Status{ocsp.StatusGood} // Old clients won't set the header, only accept good status
		} else {
			secret, err := a.Cipher.Secret(r.Context(), secretID)
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

		for _, status := range a.OCSPStatus {
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

// UnsupportedEndpoint returns 501 Not Implemented.
// To be used as the default handler for every endpoint that is not explicitly supported.
func (a *Adapter) UnsupportedEndpoint(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "unsupported endpoint", http.StatusNotImplemented)
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

// ResponseMapper returns a mapper that handles both unary and streaming vLLM responses.
// It performs usage report extraction and encryption.
func (a *Adapter) ResponseMapper(
	encryptMutator *forwarder.MutatingReader,
	extractUnaryUsage UsageExtractor,
	extractStreamingUsage UsageExtractor,
) forwarder.ResponseMapper {
	return func(resp *http.Response) (forwarder.Response, error) {
		if strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
			return a.streamingResponse(resp, encryptMutator, extractStreamingUsage), nil
		}
		return a.unaryResponse(resp, encryptMutator, extractUnaryUsage)
	}
}

func (a *Adapter) unaryResponse(
	usResp *http.Response,
	encryptMutator *forwarder.MutatingReader,
	extractUsage UsageExtractor,
) (*forwarder.UnaryResponse, error) {
	dsResp, err := forwarder.ReadUnaryResponse(usResp, constants.MaxUnaryResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("reading upstream response body: %w", err)
	}

	dsResp.Header.Set("Content-Type", usResp.Header.Get("Content-Type"))

	// Extract usage from the pre-encryption body (only for successful responses).
	if dsResp.StatusCode < 400 {
		stats, err := extractUsage(dsResp.Body)
		if err != nil {
			a.Log.Warn("Failed to extract usage from response", "error", err)
		} else {
			a.Log.Info("Extracted usage stats from response", "usage", stats, "response_type", "unary")
		}
	}

	body, err := encryptMutator.Mutate(dsResp.Body)
	if err != nil {
		return nil, fmt.Errorf("encrypting response: %w", err)
	}
	dsResp.Body = body

	return dsResp, nil
}

func (a *Adapter) streamingResponse(
	usResp *http.Response,
	encryptMutator *forwarder.MutatingReader,
	extractUsage UsageExtractor,
) *forwarder.StreamingResponse {
	dsResp := forwarder.NewStreamingResponse(usResp)
	dsResp.Header.Set("Content-Type", usResp.Header.Get("Content-Type"))

	body, clone := cloneReader(dsResp.Body)
	sseReader := sse.NewReader(clone, maxSSELineBytes)
	usageReader := NewUsageSSEReader(sseReader, extractUsage, a.Log)

	go func() {
		// Termination behaviour: clone, the io.Reader underneath usageReader, eventually returns
		// an error on Read(). Either the body stream ends regularly (io.EOF), there is an error
		// on the body stream (error is passed through to clone), or body is closed explicitly
		// (io.ErrUnexpectedEOF) which always happens because of the [forwarder.StreamingResponse]
		// contract.
		// On error, this goroutine ends, though it is not awaited.

		streamCompleted := false
		for {
			_, err := usageReader.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					streamCompleted = true
				} else if !errors.Is(err, context.Canceled) && !errors.Is(err, io.ErrUnexpectedEOF) {
					// The above are expected errors when a request is cancelled -> no need to Warn
					a.Log.Warn("SSE usage extraction error", "error", err)
				}
				break
			}
		}
		a.Log.Info(
			"Extracted usage stats from response", "usage", usageReader.LatestUsage(),
			"response_type", "streaming", "stream_completed", streamCompleted,
		)
		// Drain remaining clone data to prevent blocking the main flow.
		_, _ = io.Copy(io.Discard, clone)
	}()

	encryptedBody := encryptMutator.Reader(body)

	dsResp.Body = encryptedBody

	return dsResp
}

// cloneReader returns two readers over the same byte stream.
//
// The returned original must be read to drive the underlying reader r.
// The clone receives the same bytes and errors via an [io.Pipe] as they are read from original.
// The clone blocks the original and hence must be fully read in a separate goroutine. Always
// Close() the original to ensure that clone.Read() eventually returns an error and does not block.
func cloneReader(r io.Reader) (original io.ReadCloser, clone io.Reader) {
	pr, pw := io.Pipe()
	return &cloningReader{r: r, pw: pw}, pr
}

type cloningReader struct {
	r  io.Reader
	pw *io.PipeWriter
}

func (c *cloningReader) Read(p []byte) (int, error) {
	// Similar to io.TeeReader, but also forwards errors.
	n, err := c.r.Read(p)
	if n > 0 {
		_, _ = c.pw.Write(p[:n])
	}
	if err != nil {
		c.pw.CloseWithError(err)
	}
	return n, err
}

func (c *cloningReader) Close() error {
	c.pw.CloseWithError(io.ErrUnexpectedEOF)
	return nil
}
