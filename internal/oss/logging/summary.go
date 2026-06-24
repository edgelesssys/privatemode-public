// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package logging

import (
	"context"
	"slices"
	"sync"
)

type summaryAttrsKey struct{}

// SummaryAttrs is a container for summary log fields. See [WithSummaryAttrs] for initialization.
type SummaryAttrs struct {
	mu   sync.Mutex
	args []any
}

// WithSummaryAttrs stores a fresh [SummaryAttrs] in the context and returns it alongside the
// derived context. [AddSummaryAttrs] should be used to add any attrs to the container. A nested
// WithSummaryAttrs shadows the outer one for its subtree.
func WithSummaryAttrs(ctx context.Context) (context.Context, *SummaryAttrs) {
	a := &SummaryAttrs{}
	return context.WithValue(ctx, summaryAttrsKey{}, a), a
}

// Add appends slog-style attrs (key/value) to the container.
func (a *SummaryAttrs) Add(args ...any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.args = append(a.args, args...)
}

// Args returns a copy of the accumulated attrs as args for use in a slog call.
func (a *SummaryAttrs) Args() []any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.args)
}

// AddSummaryAttrs appends slog-style attrs (key/value) to the [SummaryAttrs] in ctx,
// or is a no-op if none is present.
func AddSummaryAttrs(ctx context.Context, args ...any) {
	if a, ok := ctx.Value(summaryAttrsKey{}).(*SummaryAttrs); ok {
		a.Add(args...)
	}
}
