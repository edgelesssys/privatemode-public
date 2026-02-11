// Package inference provides shared functionality for inference API adapters.
package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/ocsp"
	"github.com/edgelesssys/continuum/internal/oss/ocspheader"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

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
	Forward(http.ResponseWriter, *http.Request, forwarder.RequestMutator, forwarder.ResponseMutator, forwarder.HeaderMutator, ...forwarder.Opts)
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
