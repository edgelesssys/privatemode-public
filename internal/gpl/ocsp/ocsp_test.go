package ocsp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusFromString(t *testing.T) {
	tests := []struct {
		input    string
		expected Status
		wantErr  bool
	}{
		{"GOOD", StatusGood, false},
		{"REVOKED", StatusRevoked, false},
		{"UNKNOWN", StatusUnknown, false},
		{"invalid", "", true},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)
			result, err := StatusFromString(test.input)
			if test.wantErr {
				require.Error(err)
			} else {
				require.NoError(err)
				assert.Equal(test.expected, result)
			}
		})
	}
}

func TestCombineStatuses(t *testing.T) {
	tests := map[string]struct {
		input    []Status
		expected Status
	}{
		"all good":                           {[]Status{StatusGood, StatusGood}, StatusGood},
		"revoked takes precedence":           {[]Status{StatusGood, StatusRevoked, StatusUnknown}, StatusRevoked},
		"unknown takes precedence over good": {[]Status{StatusGood, StatusUnknown}, StatusUnknown},
		"no statuses":                        {[]Status{}, StatusGood},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			assert := assert.New(t)
			result := CombineStatuses(test.input)
			assert.Equal(test.expected, result)
		})
	}
}
