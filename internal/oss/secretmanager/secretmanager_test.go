// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package secretmanager

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	testclock "k8s.io/utils/clock/testing"
)

func TestGetSecretUpdatesAfterLifetime(t *testing.T) {
	assert := assert.New(t)
	secretLifetime := 1 * time.Hour
	refreshBuffer := 5 * time.Minute
	now := time.Date(2024, 0, 0, 0, 0, 0, 0, time.UTC)
	clock := testclock.NewFakeClock(now)

	mock := &updateCounter{}
	sut := New(mock.UpdateFn, secretLifetime, refreshBuffer)
	sut.clock = clock
	ctx := t.Context()

	secret, err := sut.LatestSecret(ctx)
	assert.NoError(err)
	assert.Equal(1, mock.isCalled)

	clock.Step(1 * time.Minute)
	secret2, err := sut.LatestSecret(ctx)
	// assert no update
	assert.NoError(err)
	assert.Equal(1, mock.isCalled)
	assert.Equal(secret, secret2)

	clock.Step(secretLifetime)
	secret3, err := sut.LatestSecret(ctx)
	// assert update
	assert.NoError(err)
	assert.Equal(2, mock.isCalled)
	assert.NotEqual(secret, secret3)

	clock.Step(secretLifetime - refreshBuffer)
	secret4, err := sut.LatestSecret(ctx)
	// assert update
	assert.NoError(err)
	assert.Equal(3, mock.isCalled)
	assert.NotEqual(secret3, secret4)
}

type updateCounter struct {
	isCalled int
}

func (f *updateCounter) UpdateFn(_ context.Context, _ map[string][]byte, _ time.Duration) error {
	f.isCalled++
	return nil
}
