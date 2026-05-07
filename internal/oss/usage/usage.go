// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package usage provides types for tracking inference usage statistics.
package usage

// Stats contains the usage statistics for a request.
type Stats struct {
	PromptTokens       int64
	CachedPromptTokens int64
	CompletionTokens   int64
	AudioSeconds       int64
}

// Add the values of another [Stats] and return the result.
func (s Stats) Add(other Stats) Stats {
	return Stats{
		PromptTokens:       s.PromptTokens + other.PromptTokens,
		CachedPromptTokens: s.CachedPromptTokens + other.CachedPromptTokens,
		CompletionTokens:   s.CompletionTokens + other.CompletionTokens,
		AudioSeconds:       s.AudioSeconds + other.AudioSeconds,
	}
}
