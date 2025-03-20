// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package secretmanager

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetSecretUpdatesAfterLifetime(t *testing.T) {
	assert := assert.New(t)
	secretLifetime := 1 * time.Hour
	refreshBuffer := 5 * time.Minute
	mock := &updateCounter{}
	sut := New(mock.UpdateFn, secretLifetime, refreshBuffer)
	now := time.Now()
	ctx := t.Context()

	secret, err := sut.LatestSecret(ctx, now)
	assert.NoError(err)
	assert.Equal(1, mock.isCalled)

	secret2, err := sut.LatestSecret(ctx, now.Add(1*time.Minute))
	// assert no update
	assert.NoError(err)
	assert.Equal(1, mock.isCalled)
	assert.Equal(secret, secret2)

	secret3, err := sut.LatestSecret(ctx, now.Add(secretLifetime))
	// assert update
	assert.NoError(err)
	assert.Equal(2, mock.isCalled)
	assert.NotEqual(secret, secret3)

	secret4, err := sut.LatestSecret(ctx, now.Add(2*secretLifetime-refreshBuffer))
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
