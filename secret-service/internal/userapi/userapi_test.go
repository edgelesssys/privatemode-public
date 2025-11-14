package userapi

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/proto/secret-service/userapi"
	"github.com/stretchr/testify/assert"
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

type stubSecretSetter struct {
	gotSecrets map[string][]byte
	err        error
}

func (s *stubSecretSetter) SetSecrets(_ context.Context, secrets map[string][]byte, _ int64) error {
	s.gotSecrets = secrets
	return s.err
}
