// package secrets implements a thread-safe map of secrets.
package secrets

import (
	"maps"
	"sync"
)

// Secrets is a thread-safe map of secrets.
type Secrets struct {
	inferenceSecrets map[string][]byte
	rwLock           sync.RWMutex
}

// New creates a new Secrets object.
func New(initialSecrets map[string][]byte) *Secrets {
	if initialSecrets == nil {
		initialSecrets = make(map[string][]byte)
	}
	return &Secrets{
		inferenceSecrets: maps.Clone(initialSecrets),
		rwLock:           sync.RWMutex{},
	}
}

// Get returns the secret for the given key.
func (s *Secrets) Get(key string) ([]byte, bool) {
	s.rwLock.RLock()
	defer s.rwLock.RUnlock()
	secret, ok := s.inferenceSecrets[key]
	return secret, ok
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
