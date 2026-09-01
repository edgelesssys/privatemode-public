package userapi

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hpke"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/httpapi"
	"github.com/edgelesssys/continuum/internal/oss/secretclient"
	"github.com/edgelesssys/continuum/internal/oss/secretexchange"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExchangeSecret(t *testing.T) {
	validKey, err := hpke.MLKEM768X25519().GenerateKey()
	require.NoError(t, err)
	meshPriv, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	require.NoError(t, err)

	testCases := map[string]struct {
		req          *httpapi.SecretReq
		secretSetter *stubSecretSetter
		wantStatus   int
	}{
		"success": {
			req:          &httpapi.SecretReq{PublicKey: validKey.PublicKey().Bytes()},
			secretSetter: &stubSecretSetter{},
			wantStatus:   http.StatusOK,
		},
		"empty key": {
			req:          &httpapi.SecretReq{PublicKey: nil},
			secretSetter: &stubSecretSetter{},
			wantStatus:   http.StatusBadRequest,
		},
		"invalid key": {
			req:          &httpapi.SecretReq{PublicKey: []byte{2, 3}},
			secretSetter: &stubSecretSetter{},
			wantStatus:   http.StatusBadRequest,
		},
		"secret setter error": {
			req:          &httpapi.SecretReq{PublicKey: validKey.PublicKey().Bytes()},
			secretSetter: &stubSecretSetter{err: assert.AnError},
			wantStatus:   http.StatusInternalServerError,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			s := &Server{
				secretStore: tc.secretSetter,
				log:         slog.Default(),
				meshCertRaw: []byte("meshcert"),
				meshPriv:    meshPriv,
			}

			var body bytes.Buffer
			require.NoError(json.NewEncoder(&body).Encode(tc.req))
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, secretclient.SecretPath, &body)
			recorder := httptest.NewRecorder()
			s.handler().ServeHTTP(recorder, req)

			assert.Equal(tc.wantStatus, recorder.Code)
			if tc.wantStatus != http.StatusOK {
				return
			}
			var resp httpapi.SecretResp
			require.NoError(json.NewDecoder(recorder.Body).Decode(&resp))

			assert.EqualValues("meshcert", resp.MeshCert)
			require.True(ecdsa.VerifyASN1(&meshPriv.PublicKey, secretexchange.Hash(tc.req.PublicKey, resp.EncapsulatedKey), resp.Signature))

			recipient, err := hpke.NewRecipient(resp.EncapsulatedKey, validKey, hpke.HKDFSHA256(), hpke.ExportOnly(), nil)
			require.NoError(err)
			secret, err := recipient.Export("", 32)
			require.NoError(err)

			require.Len(tc.secretSetter.gotSecrets, 1)
			assert.Equal(secret, tc.secretSetter.gotSecrets[secretexchange.ID(tc.req.PublicKey)])
			assert.Equal(int64(constants.InferenceSecretTTL/time.Second), tc.secretSetter.gotTTL)
		})
	}
}

type stubSecretSetter struct {
	gotSecrets map[string][]byte
	gotTTL     int64
	err        error
}

func (s *stubSecretSetter) SetSecrets(_ context.Context, secrets map[string][]byte, ttl int64) error {
	s.gotSecrets = secrets
	s.gotTTL = ttl
	return s.err
}
