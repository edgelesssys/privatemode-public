// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package secretmanager

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSecretUpdatesAfterLifetime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		assert := assert.New(t)
		require := require.New(t)

		mock := &updateCounter{}
		sut := New(mock.UpdateFn, false)
		ctx := t.Context()
		require.NoError(sut.OfferCredential(ctx, auth.APIKey("apikey")))

		secret, err := sut.LatestSecret(ctx)
		require.NoError(err)
		assert.Equal(1, mock.calls())

		time.Sleep(1 * time.Minute)
		secret2, err := sut.LatestSecret(ctx)
		// assert no update
		require.NoError(err)
		assert.Equal(1, mock.calls())
		assert.Equal(secret, secret2)

		time.Sleep(secretLifetime)
		secret3, err := sut.LatestSecret(ctx)
		// assert update
		require.NoError(err)
		assert.Equal(2, mock.calls())
		assert.NotEqual(secret, secret3)

		time.Sleep(secretLifetime - secretRefreshBuffer)
		secret4, err := sut.LatestSecret(ctx)
		// assert update
		require.NoError(err)
		assert.Equal(3, mock.calls())
		assert.NotEqual(secret3, secret4)
	})
}

func TestRun(t *testing.T) {
	t.Run("waits for a credential and refreshes at expiration", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			mock := &updateCounter{}
			sut := New(mock.UpdateFn, false)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			go sut.Run(ctx, slog.New(slog.DiscardHandler))

			synctest.Wait()
			require.Zero(t, mock.calls())

			sut.SetCredential(auth.APIKey("apikey"))
			synctest.Wait()
			require.Equal(t, 1, mock.calls())
			require.Equal(t, auth.APIKey("apikey"), mock.lastCredential())

			time.Sleep(secretLifetime - secretRefreshBuffer - time.Nanosecond)
			synctest.Wait()
			require.Equal(t, 1, mock.calls())

			time.Sleep(time.Nanosecond)
			synctest.Wait()
			require.Equal(t, 2, mock.calls())
			require.Equal(t, auth.APIKey("apikey"), mock.lastCredential())
		})
	})

	t.Run("backs off on errors and resets after success", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			updateErr := errors.New("update failed")
			mock := &updateCounter{}
			mock.setError(updateErr)
			sut := New(mock.UpdateFn, false)
			sut.SetCredential(auth.APIKey("apikey"))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			go sut.Run(ctx, slog.New(slog.DiscardHandler))
			synctest.Wait()
			require.Equal(t, 1, mock.calls())

			for i, step := range []struct {
				wait time.Duration
				err  error
			}{
				{5 * time.Second, updateErr},
				{10 * time.Second, updateErr},
				{20 * time.Second, updateErr},
				{40 * time.Second, updateErr},
				{60 * time.Second, updateErr},
				{60 * time.Second, nil},
				{secretLifetime - secretRefreshBuffer, updateErr},
				{5 * time.Second, nil},
			} {
				mock.setError(step.err)
				time.Sleep(step.wait - time.Nanosecond)
				synctest.Wait()
				require.Equal(t, i+1, mock.calls(), "update before wait %s elapsed", step.wait)

				time.Sleep(time.Nanosecond)
				synctest.Wait()
				require.Equal(t, i+2, mock.calls(), "update after wait %s elapsed", step.wait)
			}
		})
	})

	for _, state := range []string{"waiting for credential", "waiting for expiration", "backing off", "updating"} {
		t.Run("cancellation while "+state, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				sut := New(func(ctx context.Context, _ auth.Credential) (string, []byte, error) {
					switch state {
					case "backing off":
						return "", nil, errors.New("update failed")
					case "updating":
						<-ctx.Done()
						return "", nil, ctx.Err()
					default:
						return "secret", []byte("data"), nil
					}
				}, false)
				if state != "waiting for credential" {
					sut.SetCredential(auth.APIKey("apikey"))
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan struct{})
				go func() {
					sut.Run(ctx, slog.New(slog.DiscardHandler))
					close(done)
				}()
				synctest.Wait()
				select {
				case <-done:
					t.Fatal("Run returned before cancellation")
				default:
				}

				cancel()
				synctest.Wait()
				select {
				case <-done:
				default:
					t.Fatal("Run did not return after cancellation")
				}
			})
		})
	}
}

func TestSetCredential(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		assert := assert.New(t)
		require := require.New(t)

		mock := &updateCounter{}
		sut := New(mock.UpdateFn, false)
		ctx := t.Context()

		// Without a credential, no secret can be obtained.
		_, err := sut.LatestSecret(ctx)
		require.Error(err)

		// SetCredential makes a credential available without triggering an update itself.
		sut.SetCredential(auth.APIKey("first"))
		assert.Equal(0, mock.calls())

		// The next secret update uses the credential that was set.
		_, err = sut.LatestSecret(ctx)
		require.NoError(err)
		assert.Equal(1, mock.calls())
		assert.Equal(auth.APIKey("first"), mock.lastCredential())

		// Unlike OfferCredential, SetCredential overwrites an already-set credential.
		sut.SetCredential(auth.APIKey("second"))
		require.NoError(sut.ForceUpdate(ctx))
		assert.Equal(2, mock.calls())
		assert.Equal(auth.APIKey("second"), mock.lastCredential())
	})
}

type updateCounter struct {
	mu       sync.Mutex
	isCalled int
	lastCred auth.Credential
	err      error
}

func (f *updateCounter) UpdateFn(_ context.Context, cred auth.Credential) (string, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.isCalled++
	f.lastCred = cred
	return "", nil, f.err
}

func (f *updateCounter) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.isCalled
}

func (f *updateCounter) lastCredential() auth.Credential {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastCred
}

func (f *updateCounter) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}
