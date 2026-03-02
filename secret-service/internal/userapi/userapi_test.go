package userapi

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hpke"
	"crypto/rand"
	"log/slog"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/proto/secret-service/userapi"
	"github.com/edgelesssys/continuum/internal/oss/secretexchange"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetSecrets(t *testing.T) {
	testCases := map[string]struct {
		secretReq    *userapi.SetSecretsRequest
		secretSetter *stubSecretSetter
		wantErr      bool
	}{
		"success": {
			secretReq: &userapi.SetSecretsRequest{
				Secrets: map[string][]byte{
					"16-bytes": bytes.Repeat([]byte{0x01}, 16),
					"24-bytes": bytes.Repeat([]byte{0x01}, 24),
					"32-bytes": bytes.Repeat([]byte{0x01}, 32),
				},
			},
			secretSetter: &stubSecretSetter{},
		},
		"invalid secret length": {
			secretReq: &userapi.SetSecretsRequest{
				Secrets: map[string][]byte{
					"invalid": bytes.Repeat([]byte{0x01}, 17),
				},
			},
			secretSetter: &stubSecretSetter{},
			wantErr:      true,
		},
		"secret setter error": {
			secretReq: &userapi.SetSecretsRequest{
				Secrets: map[string][]byte{
					"16-bytes": bytes.Repeat([]byte{0x01}, 16),
					"24-bytes": bytes.Repeat([]byte{0x01}, 24),
					"32-bytes": bytes.Repeat([]byte{0x01}, 32),
				},
			},
			secretSetter: &stubSecretSetter{err: assert.AnError},
			wantErr:      true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			s := &Server{
				secretStore: tc.secretSetter,
				log:         slog.Default(),
			}

			_, err := s.SetSecrets(t.Context(), tc.secretReq)
			if tc.wantErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)
			assert.Equal(tc.secretReq.Secrets, tc.secretSetter.gotSecrets)
		})
	}
}

func TestExchangeSecret(t *testing.T) {
	validKey, err := hpke.MLKEM768X25519().GenerateKey()
	require.NoError(t, err)
	meshPriv, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	require.NoError(t, err)

	testCases := map[string]struct {
		req          *userapi.ExchangeSecretRequest
		secretSetter *stubSecretSetter
		wantErr      bool
	}{
		"success": {
			req:          &userapi.ExchangeSecretRequest{PublicKey: validKey.PublicKey().Bytes()},
			secretSetter: &stubSecretSetter{},
		},
		"empty key": {
			req:          &userapi.ExchangeSecretRequest{PublicKey: nil},
			secretSetter: &stubSecretSetter{},
			wantErr:      true,
		},
		"invalid key": {
			req:          &userapi.ExchangeSecretRequest{PublicKey: []byte{2, 3}},
			secretSetter: &stubSecretSetter{},
			wantErr:      true,
		},
		"secret setter error": {
			req:          &userapi.ExchangeSecretRequest{PublicKey: validKey.PublicKey().Bytes()},
			secretSetter: &stubSecretSetter{err: assert.AnError},
			wantErr:      true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			s := &Server{
				secretStore: tc.secretSetter,
				meshCertRaw: []byte("meshcert"),
				meshPriv:    meshPriv,
			}

			resp, err := s.ExchangeSecret(t.Context(), tc.req)
			if tc.wantErr {
				assert.Error(err)
				return
			}
			require.NoError(err)

			assert.EqualValues("meshcert", resp.MeshCert)
			require.True(ecdsa.VerifyASN1(&meshPriv.PublicKey, secretexchange.Hash(tc.req.PublicKey, resp.EncapsulatedKey), resp.Signature))

			recipient, err := hpke.NewRecipient(resp.EncapsulatedKey, validKey, hpke.HKDFSHA256(), hpke.ExportOnly(), nil)
			require.NoError(err)
			secret, err := recipient.Export("", 32)
			require.NoError(err)

			require.Len(tc.secretSetter.gotSecrets, 1)
			assert.Equal(secret, tc.secretSetter.gotSecrets[secretexchange.ID(tc.req.PublicKey)])
		})
	}
}

type stubSecretSetter struct {
	gotSecrets map[string][]byte
	err        error
}

func (s *stubSecretSetter) SetSecrets(_ context.Context, secrets map[string][]byte, _ int64) error {
	s.gotSecrets = secrets
	return s.err
}
