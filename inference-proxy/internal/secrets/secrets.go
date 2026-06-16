// Package secrets implements a thread-safe map of secrets.
package secrets

import (
	"context"
	"sync"
	"time"
)

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

// Get returns the secret for the given key.
func (s *Secrets) Get(ctx context.Context, key string) ([]byte, bool) {
	s.rwLock.RLock()
	secret, ok := s.inferenceSecrets[key]
	s.rwLock.RUnlock()
	if ok {
		return secret, true
	}

	// The etcd watch mechanism we use to populate the secret cache
	// may not have been triggered yet
	// In that case, try to retrieve the secret directly from etcd
	etcdCtx, cancel := context.WithTimeout(ctx, 15*time.Second) // Connection should be almost instant if there are no issues
	defer cancel()
	secretValue, err := s.secretGetter.GetSecret(etcdCtx, key)
	if err != nil {
		return nil, false
	}
	return secretValue, true
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
	GetSecret(ctx context.Context, key string) ([]byte, error)
}
