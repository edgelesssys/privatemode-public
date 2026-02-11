// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package secretmanager

import (
	"context"
	"testing"
	"time"

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
	require.NoError(sut.OfferAPIKey(ctx, "apikey"))

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

type updateCounter struct {
	isCalled int
}

func (f *updateCounter) UpdateFn(context.Context, string) (string, []byte, error) {
	f.isCalled++
	return "", nil, nil
}
