package inference

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/adaptertest"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/ocsp"
	"github.com/edgelesssys/continuum/internal/oss/ocspheader"
	"github.com/edgelesssys/continuum/internal/oss/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForwardEncryptedRemoteResourceURLError(t *testing.T) {
	const domainError = `{"error":{"message":"The URL must be from one of the allowed domains: ['/']. Input URL domain: example.com","type":"BadRequestError","param":null,"code":400},"request_id":"test"}`
	rewrittenError := fmt.Sprintf(`{"error":{"message":"%s","type":"BadRequestError","param":null,"code":400},"request_id":"test"}`, remoteResourceURLError)
	testCases := map[string]struct {
		status   int
		body     string
		stream   bool
		expected string
	}{
		"domain error": {
			status: http.StatusBadRequest, body: domainError, expected: rewrittenError,
		},
		"domain error for streaming request": {
			status: http.StatusBadRequest, body: domainError, stream: true, expected: rewrittenError,
		},
		"unrelated bad request": {
			status: http.StatusBadRequest, body: `{"error":{"type":"BadRequestError","message":"Invalid model"}}`,
		},
		"different error type": {
			status: http.StatusBadRequest, body: strings.Replace(domainError, "BadRequestError", "InternalServerError", 1),
		},
		"different status": {
			status: http.StatusInternalServerError, body: domainError,
		},
		"successful response": {
			status: http.StatusOK, body: domainError,
		},
		"malformed JSON": {
			status: http.StatusBadRequest, body: domainError[:len(domainError)-1],
		},
		"non-string message": {
			status: http.StatusBadRequest, body: `{"error":{"type":"BadRequestError","message":["URL must be from one of the allowed domains"]}}`,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			log := slog.Default()
			adapter := &Adapter{
				EncForwarder: adaptertest.EncForwarder{Inner: forwarder.New(srv.Client(), srv.Listener.Addr().String(), forwarder.SchemeHTTP, forwarder.UpstreamModel, log)},
				Log:          log,
			}
			body := `{"stream":false}`
			if tc.stream {
				body = `{"stream":true}`
			}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, constants.OpenAIChatCompletionsEndpoint, strings.NewReader(body))
			response := httptest.NewRecorder()
			noUsage := func([]byte) (usage.Stats, error) { return usage.Stats{}, ErrNoUsage }
			adapter.ForwardEncrypted(response, request, forwarder.NoRequestMutation, nil, nil, noUsage, noUsage)
			assert.Equal(t, tc.status, response.Code)
			assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
			if tc.expected != "" {
				assert.JSONEq(t, tc.expected, response.Body.String())
			} else {
				assert.Equal(t, tc.body, response.Body.String())
			}
		})
	}
}

func TestVerifyOCSP(t *testing.T) {
	gpuPolicyFailure := "GPU attestation returned a GPU OCSP status that is not accepted by the client"
	driverPolicyFailure := "GPU attestation returned a driver OCSP status that is not accepted by the client"
	vbiosPolicyFailure := "GPU attestation returned a VBIOS OCSP status that is not accepted by the client"

	testCases := map[string]struct {
		ocspStatus     ocsp.StatusInfo
		acceptedStatus []ocspheader.AllowStatus
		secretID       string // when unset, a known secret ID is used
		expectedCode   int
		expectedBody   string
	}{
		"all good, accepted good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusOK,
		},
		"all good, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			expectedCode: http.StatusOK,
		},
		"missing secret, OCSP header set": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			secretID:       "non-existing",
			expectedCode:   http.StatusBadRequest,
			expectedBody:   constants.ErrorNoSecretForID,
		},
		"unknown gpu, accept good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusUnknown, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusInternalServerError,
			expectedBody:   gpuPolicyFailure,
		},
		"unknown driver, accept good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusUnknown},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusInternalServerError,
			expectedBody:   driverPolicyFailure,
		},
		"unknown vbios, accept good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusUnknown, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusInternalServerError,
			expectedBody:   vbiosPolicyFailure,
		},
		"revoked gpu, accept good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusRevoked(time.Now()), VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusInternalServerError,
			expectedBody:   gpuPolicyFailure,
		},
		"revoked driver, accept good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusRevoked(time.Now())},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusInternalServerError,
			expectedBody:   driverPolicyFailure,
		},
		"revoked vbios, accept good": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusRevoked(time.Now()), Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood},
			expectedCode:   http.StatusInternalServerError,
			expectedBody:   vbiosPolicyFailure,
		},
		"unknown gpu, accept unknown": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusUnknown, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown},
			expectedCode:   http.StatusOK,
		},
		"unknown driver, accept unknown": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusUnknown},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown},
			expectedCode:   http.StatusOK,
		},
		"unknown vbios, accept unknown": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusUnknown, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown},
			expectedCode:   http.StatusOK,
		},
		"revoked gpu, accept revoked": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusRevoked(time.Now()), VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown, ocspheader.AllowStatusRevoked},
			expectedCode:   http.StatusOK,
		},
		"revoked driver, accept revoked": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusRevoked(time.Now())},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown, ocspheader.AllowStatusRevoked},
			expectedCode:   http.StatusOK,
		},
		"revoked vbios, accept revoked": {
			ocspStatus:     ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusRevoked(time.Now()), Driver: ocsp.StatusGood},
			acceptedStatus: []ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown, ocspheader.AllowStatusRevoked},
			expectedCode:   http.StatusOK,
		},
		"unknown gpu, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusUnknown, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			expectedCode: http.StatusInternalServerError,
			expectedBody: gpuPolicyFailure,
		},
		"unknown driver, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusUnknown},
			expectedCode: http.StatusInternalServerError,
			expectedBody: driverPolicyFailure,
		},
		"unknown vbios, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusUnknown, Driver: ocsp.StatusGood},
			expectedCode: http.StatusInternalServerError,
			expectedBody: vbiosPolicyFailure,
		},
		"revoked gpu, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusRevoked(time.Now()), VBIOS: ocsp.StatusGood, Driver: ocsp.StatusGood},
			expectedCode: http.StatusInternalServerError,
			expectedBody: gpuPolicyFailure,
		},
		"revoked driver, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusGood, Driver: ocsp.StatusRevoked(time.Now())},
			expectedCode: http.StatusInternalServerError,
			expectedBody: driverPolicyFailure,
		},
		"revoked vbios, no header set": {
			ocspStatus:   ocsp.StatusInfo{GPU: ocsp.StatusGood, VBIOS: ocsp.StatusRevoked(time.Now()), Driver: ocsp.StatusGood},
			expectedCode: http.StatusInternalServerError,
			expectedBody: vbiosPolicyFailure,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			secret := bytes.Repeat([]byte{0x01}, 32)
			secretID := "test"

			a := &Adapter{
				Secrets: &adaptertest.Secrets{
					SecretMap: map[string][]byte{secretID: secret},
				},
				EncForwarder:  adaptertest.EncForwarder{},
				WorkloadTasks: []string{"generate"},
				Log:           slog.Default(),
				OCSPStatus:    []ocsp.StatusInfo{tc.ocspStatus},
			}

			// Create a simple handler that returns 200 OK
			innerHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			handler := a.VerifyOCSP(innerHandler)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/test", http.NoBody)

			if tc.acceptedStatus != nil {
				ocspHeader := ocspheader.NewHeader(tc.acceptedStatus, time.Time{})
				policyHeader, err := ocspHeader.Marshal()
				require.NoError(err)
				policyMACHeader, err := ocspHeader.MarshalMACHeader([32]byte(secret))
				require.NoError(err)

				sentSecretID := secretID
				if tc.secretID != "" {
					sentSecretID = tc.secretID
				}
				request.Header.Set(constants.PrivatemodeNvidiaOCSPPolicyHeader, policyHeader)
				request.Header.Set(constants.PrivatemodeNvidiaOCSPPolicyMACHeader, policyMACHeader)
				request.Header.Set(constants.PrivatemodeSecretIDHeader, sentSecretID)
			}

			responseRecorder := httptest.NewRecorder()
			handler.ServeHTTP(responseRecorder, request)

			assert.Equal(tc.expectedCode, responseRecorder.Code)
			assert.Contains(responseRecorder.Body.String(), tc.expectedBody)
		})
	}
}

func TestUnsupportedEndpoint(t *testing.T) {
	assert := assert.New(t)

	a := &Adapter{
		Log: slog.Default(),
	}

	responseRecorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/unsupported", http.NoBody)

	a.UnsupportedEndpoint(responseRecorder, request)

	assert.Equal(http.StatusNotImplemented, responseRecorder.Code)
	assert.Contains(responseRecorder.Body.String(), "unsupported endpoint")
}
