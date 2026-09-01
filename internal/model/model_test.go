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
		"gpt oss latest to current": {
			input:    "gpt-oss-latest",
			expected: "openai/gpt-oss-120b",
		},
		"kimi latest to current": {
			input:    "kimi-latest",
			expected: "kimi-k2.6",
		},
		"glm latest to current": {
			input:    "glm-latest",
			expected: "glm-5.3",
		},
		"glm predecessor to current": {
			input:    "glm-5.2",
			expected: "glm-5.3",
		},
		"whisper short to full": {
			input:    "whisper-large-v3",
			expected: "openai/whisper-large-v3",
		},
		"gpt oss short to full": {
			input:    "gpt-oss-120b",
			expected: "openai/gpt-oss-120b",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := Resolve(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
