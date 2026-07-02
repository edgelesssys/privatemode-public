// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package secretmanager

import (
	"context"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	testclock "k8s.io/utils/clock/testing"
)

func TestGetSecretUpdatesAfterLifetime(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	now := time.Date(2024, 0, 0, 0, 0, 0, 0, time.UTC)
	clock := testclock.NewFakeClock(now)

	mock := &updateCounter{}
	sut := New(mock.UpdateFn, false)
	sut.clock = clock
	ctx := t.Context()
	require.NoError(sut.OfferCredential(ctx, auth.APIKey("apikey")))

	secret, err := sut.LatestSecret(ctx)
	require.NoError(err)
	assert.Equal(1, mock.isCalled)

	clock.Step(1 * time.Minute)
	secret2, err := sut.LatestSecret(ctx)
	// assert no update
	require.NoError(err)
	assert.Equal(1, mock.isCalled)
	assert.Equal(secret, secret2)

	clock.Step(secretLifetime)
	secret3, err := sut.LatestSecret(ctx)
	// assert update
	require.NoError(err)
	assert.Equal(2, mock.isCalled)
	assert.NotEqual(secret, secret3)

	clock.Step(secretLifetime - secretRefreshBuffer)
	secret4, err := sut.LatestSecret(ctx)
	// assert update
	require.NoError(err)
	assert.Equal(3, mock.isCalled)
	assert.NotEqual(secret3, secret4)
}

func TestSetCredential(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	now := time.Date(2024, 0, 0, 0, 0, 0, 0, time.UTC)
	clock := testclock.NewFakeClock(now)

	mock := &updateCounter{}
	sut := New(mock.UpdateFn, false)
	sut.clock = clock
	ctx := t.Context()

	// Without a credential, no secret can be obtained.
	_, err := sut.LatestSecret(ctx)
	require.Error(err)

	// SetCredential makes a credential available without triggering an update itself.
	sut.SetCredential(auth.APIKey("first"))
	assert.Equal(0, mock.isCalled)

	// The next secret update uses the credential that was set.
	_, err = sut.LatestSecret(ctx)
	require.NoError(err)
	assert.Equal(1, mock.isCalled)
	assert.Equal(auth.APIKey("first"), mock.lastCred)

	// Unlike OfferCredential, SetCredential overwrites an already-set credential.
	sut.SetCredential(auth.APIKey("second"))
	require.NoError(sut.ForceUpdate(ctx))
	assert.Equal(2, mock.isCalled)
	assert.Equal(auth.APIKey("second"), mock.lastCred)
}

type updateCounter struct {
	isCalled int
	lastCred auth.Credential
}

func (f *updateCounter) UpdateFn(_ context.Context, cred auth.Credential) (string, []byte, error) {
	f.isCalled++
	f.lastCred = cred
	return "", nil, nil
}
