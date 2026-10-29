package inference

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/adaptertest"
	"github.com/edgelesssys/continuum/inference-proxy/internal/encforward"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/encbody"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/mutators"
	"github.com/edgelesssys/continuum/internal/oss/ocsp"
	"github.com/edgelesssys/continuum/internal/oss/ocspheader"
	"github.com/edgelesssys/continuum/internal/oss/persist"
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

			err := a.verifyOCSP(request, nil)
			assert.Equal(tc.expectedCode, forwarder.StatusFromError(err))
			if tc.expectedBody != "" {
				require.Error(err)
				assert.Contains(err.Error(), tc.expectedBody)
			} else {
				require.NoError(err)
			}
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

func TestAuthenticatedOCSPPolicy(t *testing.T) {
	secret := bytes.Repeat([]byte{1}, 32)
	now := time.Now().Truncate(time.Second)
	marshalPolicy := func(statuses []ocspheader.AllowStatus, revokedNbf time.Time) string {
		policy, err := ocspheader.NewHeader(statuses, revokedNbf).Marshal()
		require.NoError(t, err)
		return policy
	}
	good := marshalPolicy([]ocspheader.AllowStatus{ocspheader.AllowStatusGood}, time.Time{})
	unknown := marshalPolicy([]ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown}, time.Time{})
	revoked := marshalPolicy([]ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusRevoked}, now.Add(-time.Hour))
	legacy := ocspheader.NewHeader([]ocspheader.AllowStatus{ocspheader.AllowStatusGood, ocspheader.AllowStatusUnknown}, time.Time{})
	legacyMAC, err := legacy.MarshalMACHeader([32]byte(secret))
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		clientVersion string
		headers       map[string]string
		outerPolicy   string
		outerMAC      string
		gpu           ocsp.Status
		code          int
		tamper        bool
	}{
		"v1.59 uses legacy policy":                         {clientVersion: "v1.59.9", outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusOK},
		"v1.59 without policy defaults to good":            {clientVersion: "v1.59.9", gpu: ocsp.StatusGood, code: http.StatusOK},
		"v1.60 uses legacy policy":                         {clientVersion: "v1.60.0", outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusOK},
		"v1.60 without policy defaults to good":            {clientVersion: "v1.60.9", gpu: ocsp.StatusGood, code: http.StatusOK},
		"short v1.60 uses legacy policy":                   {clientVersion: "v1.60", outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusOK},
		"v1.60 prerelease uses legacy policy":              {clientVersion: "v1.60.0-pre", outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusOK},
		"missing version uses legacy policy":               {outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusOK},
		"invalid version uses legacy policy":               {clientVersion: "invalid", outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusOK},
		"v1.61 requires authenticated policy":              {clientVersion: "v1.61.0", gpu: ocsp.StatusGood, code: http.StatusBadRequest},
		"v1.61 cannot use legacy policy":                   {clientVersion: "v1.61.0", outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusBadRequest},
		"short v1.61 requires authenticated policy":        {clientVersion: "v1.61", gpu: ocsp.StatusGood, code: http.StatusBadRequest},
		"v1.61 prerelease requires authenticated policy":   {clientVersion: "v1.61.0-pre", gpu: ocsp.StatusGood, code: http.StatusBadRequest},
		"newer minor requires authenticated policy":        {clientVersion: "v1.62.0", gpu: ocsp.StatusGood, code: http.StatusBadRequest},
		"newer major requires authenticated policy":        {clientVersion: "v2.0.0", gpu: ocsp.StatusGood, code: http.StatusBadRequest},
		"good only default":                                {gpu: ocsp.StatusGood, code: http.StatusOK},
		"default rejects unknown":                          {gpu: ocsp.StatusUnknown, code: http.StatusInternalServerError},
		"authenticated unknown":                            {clientVersion: "v1.61.0", headers: map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: unknown}, gpu: ocsp.StatusUnknown, code: http.StatusOK},
		"authenticated overrides restrictive outer policy": {clientVersion: "v1.61.0", headers: map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: unknown}, outerPolicy: good, outerMAC: "invalid", gpu: ocsp.StatusUnknown, code: http.StatusOK},
		"authenticated overrides permissive legacy policy": {clientVersion: "v1.61.0", headers: map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: good}, outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusInternalServerError},
		"downgraded version respects authenticated policy": {clientVersion: "v1.60.0", headers: map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: good}, outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusInternalServerError},
		"missing version respects authenticated policy":    {headers: map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: good}, outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusInternalServerError},
		"old version accepts authenticated unknown":        {clientVersion: "v1.60.0", headers: map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: unknown}, gpu: ocsp.StatusUnknown, code: http.StatusOK},
		"old version rejects empty authenticated policy":   {clientVersion: "v1.60.0", headers: map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: ""}, outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusBadRequest},
		"old version rejects invalid authenticated policy": {clientVersion: "v1.60.0", headers: map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: "invalid"}, outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusBadRequest},
		"empty authenticated policy cannot fall back":      {clientVersion: "v1.61.0", headers: map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: ""}, outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusBadRequest},
		"malformed authenticated policy cannot fall back":  {clientVersion: "v1.61.0", headers: map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: "invalid"}, outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusBadRequest},
		"unsigned outer policy rejected":                   {outerPolicy: unknown, gpu: ocsp.StatusUnknown, code: http.StatusBadRequest},
		"legacy MAC remains supported":                     {outerPolicy: unknown, outerMAC: legacyMAC, gpu: ocsp.StatusUnknown, code: http.StatusOK},
		"invalid legacy MAC rejected":                      {outerPolicy: unknown, outerMAC: "invalid", gpu: ocsp.StatusUnknown, code: http.StatusBadRequest},
		"revoked within grace period":                      {clientVersion: "v1.61.0", headers: map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: revoked}, gpu: ocsp.StatusRevoked(now), code: http.StatusOK},
		"revoked outside grace period":                     {clientVersion: "v1.61.0", headers: map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: revoked}, gpu: ocsp.StatusRevoked(now.Add(-2 * time.Hour)), code: http.StatusInternalServerError},
		"tampered ciphertext":                              {clientVersion: "v1.61.0", headers: map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: unknown}, gpu: ocsp.StatusUnknown, code: http.StatusBadRequest, tamper: true},
	} {
		t.Run(name, func(t *testing.T) {
			var forwarded atomic.Bool
			workload := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				forwarded.Store(true)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer workload.Close()
			secrets := &adaptertest.Secrets{SecretMap: map[string][]byte{"test": secret}}
			log := slog.Default()
			a := &Adapter{
				Secrets:      secrets,
				EncForwarder: encforward.New(forwarder.New(workload.Client(), workload.Listener.Addr().String(), forwarder.SchemeHTTP, forwarder.UpstreamModel, log), secrets),
				OCSPStatus:   []ocsp.StatusInfo{{GPU: tc.gpu, Driver: ocsp.StatusGood, VBIOS: ocsp.StatusGood}},
				Log:          log,
			}
			client, err := encbody.InitializeClientExchange(secret)
			require.NoError(t, err)
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, constants.OpenAIChatCompletionsEndpoint, strings.NewReader(`{}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set(constants.PrivatemodeVersionHeader, tc.clientVersion)
			require.NoError(t, mutators.ContentLengthTokensInjector(r))
			r.Header.Set(constants.PrivatemodeSecretIDHeader, "test")
			r.Header.Set(constants.PrivatemodeNvidiaOCSPPolicyHeader, tc.outerPolicy)
			r.Header.Set(constants.PrivatemodeNvidiaOCSPPolicyMACHeader, tc.outerMAC)
			require.NoError(t, client.EncryptRequest(r, tc.headers))
			if tc.tamper {
				wire, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.NoError(t, r.Body.Close())
				wire[len(wire)-1] ^= 1
				persist.SetBody(r, wire)
			}
			w := httptest.NewRecorder()
			mutated := false
			mutator := func(*http.Request) error {
				mutated = true
				return nil
			}
			noUsage := func([]byte) (usage.Stats, error) { return usage.Stats{}, ErrNoUsage }
			a.ForwardEncrypted(w, r, mutator, nil, nil, noUsage, noUsage)
			assert.Equal(t, tc.code, w.Code, w.Body.String())
			assert.Equal(t, tc.code == http.StatusOK, forwarded.Load())
			assert.Equal(t, tc.code == http.StatusOK, mutated)
		})
	}
}
