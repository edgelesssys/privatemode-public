// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package updater

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/edgelesssys/continuum/internal/gpl/logging"
	"github.com/stretchr/testify/assert"
)

func TestUpdateSecrets(t *testing.T) {
	testCases := map[string]struct {
		wantErr   bool
		customize func(*Updater)
	}{
		"succeeds after client TLS config is updated": {},
		"fails when TLS config update fails": {
			customize: func(sut *Updater) {
				sut.tlsConfigGetter = &stubTLSCfgUpdater{
					err: errors.New("failed to update TLS config"),
				}
			},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			log := logging.NewLogger(logging.DefaultFlagValue)
			secretClient := &stubSecretClient{
				errWithoutTLSCfg: errors.New("need tls update"),
				tlsConfig:        nil,
			}
			sut := &Updater{
				ssClient:        secretClient,
				tlsConfigGetter: &stubTLSCfgUpdater{},
				log:             log,
				retryOpts:       []retry.Option{retry.Delay(time.Millisecond), retry.Attempts(2)},
			}
			if tc.customize != nil {
				tc.customize(sut)
			}

			err := sut.UpdateSecrets(context.Background(), map[string][]byte{"key": []byte("value")}, time.Hour)
			assert := assert.New(t)
			if tc.wantErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)
		})
	}
}

type stubTLSCfgUpdater struct {
	err error
}

func (t *stubTLSCfgUpdater) GetTLSConfig(_ context.Context) (*tls.Config, error) {
	return &tls.Config{}, t.err
}

type stubSecretClient struct {
	errWithoutTLSCfg error
	tlsConfig        *tls.Config
}

func (s *stubSecretClient) SetSecrets(_ context.Context, _ map[string][]byte, _ time.Duration) error {
	if s.tlsConfig == nil {
		return s.errWithoutTLSCfg
	}
	return nil
}

func (s *stubSecretClient) SetTLSConfig(tlsConfig *tls.Config) {
	s.tlsConfig = tlsConfig
}
