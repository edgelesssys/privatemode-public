// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package updater

import (
	"context"
	"crypto/x509"
	"testing"
	"time"

	"github.com/avast/retry-go/v5"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/stretchr/testify/assert"
)

func TestUpdateSecret(t *testing.T) {
	testCases := map[string]struct {
		wantErr   bool
		customize func(*Updater)
	}{
		"succeeds after client TLS config is updated": {},
		"fails when TLS config update fails": {
			customize: func(sut *Updater) {
				sut.caGetter = &stubCAGetter{
					err: assert.AnError,
				}
			},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			log := logging.NewLogger(logging.DefaultFlagValue)
			secretClient := &stubSecretClient{
				errWithoutCA: assert.AnError,
			}
			sut := &Updater{
				ssClient:  secretClient,
				caGetter:  &stubCAGetter{},
				log:       log,
				retryOpts: []retry.Option{retry.Delay(time.Millisecond), retry.Attempts(2)},
			}
			if tc.customize != nil {
				tc.customize(sut)
			}

			id, data, err := sut.UpdateSecret(t.Context(), "")
			assert := assert.New(t)
			if tc.wantErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)
			assert.Equal("id", id)
			assert.EqualValues("data", data)
		})
	}
}

type stubCAGetter struct {
	err error
}

func (c *stubCAGetter) GetMeshCA(context.Context, string) (*x509.Certificate, error) {
	return &x509.Certificate{}, c.err
}

type stubSecretClient struct {
	errWithoutCA error
}

func (s *stubSecretClient) ExchangeSecret(_ context.Context, meshCA *x509.Certificate, _ string) (string, []byte, error) {
	if meshCA == nil {
		return "", nil, s.errWithoutCA
	}
	return "id", []byte("data"), nil
}
