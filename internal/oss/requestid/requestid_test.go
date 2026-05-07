// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package requestid

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeString(t *testing.T) {
	testCases := map[string]struct {
		input    string
		expected string
	}{
		"valid string": {
			input:    "valid_string-123",
			expected: "valid_string-123",
		},
		"string with special characters": {
			input:    "invalid$string!with@special#characters",
			expected: "invalid?string?with?special?characters",
		},
		"string longer than 64 characters": {
			input:    "this_is_a_very_long_string_that_exceeds_sixty_four_characters_and_should_be_truncated",
			expected: "this_is_a_very_long_string_that_exceeds_sixty_four_characters_an...",
		},
		"unicode characters": {
			input:    "unicode_string_😀_with_emoji",
			expected: "unicode_string_?_with_emoji",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.expected, sanitizeString(tc.input))
		})
	}
}
