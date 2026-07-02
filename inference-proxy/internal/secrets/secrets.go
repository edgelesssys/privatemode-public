// Package secrets implements a thread-safe map of secrets.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrSecretNotFound is returned when a secret could not be found.
var ErrSecretNotFound = errors.New("secret not found")

// Secrets is a thread-safe map of secrets.
type Secrets struct {
	inferenceSecrets map[string][]byte
	secretGetter     secretGetter
	rwLock           sync.RWMutex
}

// New creates a new Secrets object.
func New(secretGetter secretGetter) *Secrets {
	secrets := make(map[string][]byte)

	return &Secrets{
		inferenceSecrets: secrets,
		secretGetter:     secretGetter,
		rwLock:           sync.RWMutex{},
	}
}

// Reset replaces the entire secrets map with the given newSecrets.
func (s *Secrets) Reset(newSecrets map[string][]byte) {
	s.rwLock.Lock()
	defer s.rwLock.Unlock()
	s.inferenceSecrets = newSecrets
}

// Get returns the secret for the given key. If no such secret exists, it returns
// [ErrSecretNotFound].
func (s *Secrets) Get(ctx context.Context, key string) ([]byte, error) {
	s.rwLock.RLock()
	secret, ok := s.inferenceSecrets[key]
	s.rwLock.RUnlock()
	if ok {
		return secret, nil
	}

	// The etcd watch mechanism we use to populate the secret cache
	// may not have been triggered yet
	// In that case, try to retrieve the secret directly from etcd
	etcdCtx, cancel := context.WithTimeout(ctx, 15*time.Second) // Connection should be almost instant if there are no issues
	defer cancel()
	secretValue, err := s.secretGetter.GetSecret(etcdCtx, key)
	if err != nil {
		return nil, fmt.Errorf("fetching secret from etcd: %w", err)
	}
	return secretValue, nil
}

// Set sets the secret for the given key.
func (s *Secrets) Set(key string, value []byte) {
	s.rwLock.Lock()
	defer s.rwLock.Unlock()
	s.inferenceSecrets[key] = value
}

// Delete deletes the secret for the given key.
func (s *Secrets) Delete(key string) {
	s.rwLock.Lock()
	defer s.rwLock.Unlock()
	delete(s.inferenceSecrets, key)
}

type secretGetter interface {
	// GetSecret retrieves the secret identified by key from the underlying store. It returns
	// [ErrSecretNotFound] if there is no such secret.
	GetSecret(ctx context.Context, key string) ([]byte, error)
}
