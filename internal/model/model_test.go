package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolve(t *testing.T) {
	tests := map[string]struct {
		input    string
		expected string
	}{
		"qwen3 stays unchanged": {
			input:    "qwen3-coder-30b-a3b",
			expected: "qwen3-coder-30b-a3b",
		},
		"whisper short to full": {
			input:    "whisper-large-v3",
			expected: "openai/whisper-large-v3",
		},
		"whisper full stays unchanged": {
			input:    "openai/whisper-large-v3",
			expected: "openai/whisper-large-v3",
		},
		"unknown stays unchanged": {
			input:    "some-random-model",
			expected: "some-random-model",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := Resolve(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
