package ocsp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCombineStatuses(t *testing.T) {
	tests := map[string]struct {
		input    []Status
		expected Status
	}{
		"all good": {[]Status{StatusGood, StatusGood}, StatusGood},
		"revoked takes precedence": {
			[]Status{StatusGood, StatusRevoked(time.Time{}.Add(time.Hour)), StatusUnknown},
			StatusRevoked(time.Time{}.Add(time.Hour)),
		},
		"oldest revoked status takes precedence": {
			[]Status{StatusRevoked(time.Time{}.Add(5 * time.Hour)), StatusRevoked(time.Time{}.Add(1 * time.Hour)), StatusRevoked(time.Time{}.Add(3 * time.Hour))},
			StatusRevoked(time.Time{}.Add(1 * time.Hour)),
		},
		"unknown takes precedence over good": {[]Status{StatusGood, StatusUnknown}, StatusUnknown},
		"no statuses":                        {[]Status{}, StatusGood},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			result := CombineStatuses(tc.input)
			assert.Equal(tc.expected, result)
		})
	}
}

func TestStatusAcceptedBy(t *testing.T) {
	tests := map[string]struct {
		status          Status
		allowedStatuses []Status
		expected        bool
	}{
		"good allowed": {
			status:          StatusGood,
			allowedStatuses: []Status{StatusGood, StatusUnknown},
			expected:        true,
		},
		"unknown allowed": {
			status:          StatusUnknown,
			allowedStatuses: []Status{StatusGood, StatusUnknown},
			expected:        true,
		},
		"revoked allowed": {
			status:          StatusRevoked(time.Time{}.Add(time.Hour)),
			allowedStatuses: []Status{StatusGood, StatusUnknown, StatusRevoked(time.Time{}.Add(time.Hour))},
			expected:        true,
		},
		"unknown not allowed": {
			status:          StatusUnknown,
			allowedStatuses: []Status{StatusGood},
			expected:        false,
		},
		"revoked not allowed": {
			status:          StatusRevoked(time.Time{}.Add(time.Hour)),
			allowedStatuses: []Status{StatusGood, StatusUnknown},
			expected:        false,
		},
		"revoked not allowed by date": {
			status:          StatusRevoked(time.Time{}.Add(time.Hour)),
			allowedStatuses: []Status{StatusGood, StatusUnknown, StatusRevoked(time.Time{}.Add(2 * time.Hour))},
			expected:        false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			result := tc.status.AcceptedBy(tc.allowedStatuses)
			assert.Equal(tc.expected, result)
		})
	}
}
