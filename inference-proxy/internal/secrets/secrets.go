// Package secrets implements a thread-safe map of secrets.
package secrets

import (
	"context"
	"maps"
	"sync"
)

// Secrets is a thread-safe map of secrets.
type Secrets struct {
	inferenceSecrets map[string][]byte
	secretGetter     secretGetter
	rwLock           sync.RWMutex
}

// New creates a new Secrets object.
func New(secretGetter secretGetter, initialSecrets map[string][]byte) *Secrets {
	if initialSecrets == nil {
		initialSecrets = make(map[string][]byte)
	}
	return &Secrets{
		inferenceSecrets: maps.Clone(initialSecrets),
		secretGetter:     secretGetter,
		rwLock:           sync.RWMutex{},
	}
}

// Get returns the secret for the given key.
func (s *Secrets) Get(ctx context.Context, key string) ([]byte, bool) {
	s.rwLock.RLock()
	defer s.rwLock.RUnlock()
	secret, ok := s.inferenceSecrets[key]
	if ok {
		return secret, true
	}

	// The etcd watch mechanism we use to populate the secret cache
	// may not have been triggered yet
	// In that case, try to retrieve the secret directly from etcd
	secret, err := s.secretGetter.GetSecret(ctx, key)
	if err != nil {
		return nil, false
	}
	return secret, true
}

// Set sets the secret for the given key.
func (s *Secrets) Set(key string, secret []byte) {
	s.rwLock.Lock()
	defer s.rwLock.Unlock()
	s.inferenceSecrets[key] = secret
}

// Delete deletes the secret for the given key.
func (s *Secrets) Delete(key string) {
	s.rwLock.Lock()
	defer s.rwLock.Unlock()
	delete(s.inferenceSecrets, key)
}

// Keys returns the keys of the secrets.
func (s *Secrets) Keys() []string {
	s.rwLock.RLock()
	defer s.rwLock.RUnlock()
	var keys []string
	for key := range s.inferenceSecrets {
		keys = append(keys, key)
	}
	return keys
}

type secretGetter interface {
	GetSecret(ctx context.Context, key string) ([]byte, error)
}
