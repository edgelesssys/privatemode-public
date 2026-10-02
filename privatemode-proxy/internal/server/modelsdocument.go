// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/openai"
)

// ModelsDocument serves a static JSON document at the models endpoint instead of forwarding
// the request to the API. Each request stats the file and re-reads it only when its size or
// modification time changed, so edits take effect without a restart at the cost of one stat
// per request. If the file becomes unreadable or invalid, the last good document is served.
type ModelsDocument struct {
	path string
	log  *slog.Logger

	mu      sync.Mutex
	last    []byte    // last good document
	size    int64     // size of the file last is from
	modTime time.Time // modification time of the file last is from
}

// NewModelsDocument loads the document at path. It fails if the file is missing or not a models response.
func NewModelsDocument(path string, log *slog.Logger) (*ModelsDocument, error) {
	d := &ModelsDocument{path: path, log: log}
	if _, err := d.reload(); err != nil {
		return nil, err
	}
	return d, nil
}

// ServeHTTP writes the current document.
func (d *ModelsDocument) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, err := d.reload()
	if err != nil {
		d.log.ErrorContext(r.Context(), "Serving last good models document", "error", err)
		d.mu.Lock()
		data = d.last
		d.mu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// reload returns the current document, re-reading the file only if it changed since the last
// good load. A ConfigMap update replaces the file behind a symlink, so it always shows up as a
// new modification time.
func (d *ModelsDocument) reload() ([]byte, error) {
	info, err := os.Stat(d.path)
	if err != nil {
		return nil, fmt.Errorf("checking models document: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.last != nil && info.Size() == d.size && info.ModTime().Equal(d.modTime) {
		return d.last, nil
	}

	data, err := os.ReadFile(d.path)
	if err != nil {
		return nil, fmt.Errorf("reading models document: %w", err)
	}
	// The document is served verbatim, so extra fields the API doesn't return are fine; only the
	// shape clients rely on is checked.
	var doc openai.ModelsResponse
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing models document %q: %w", d.path, err)
	}
	if doc.Data == nil {
		return nil, fmt.Errorf("models document %q has no \"data\" field", d.path)
	}

	d.last, d.size, d.modTime = data, info.Size(), info.ModTime()
	return data, nil
}
