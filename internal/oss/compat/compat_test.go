// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package compat

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAtLeastMajorMinor(t *testing.T) {
	tests := map[string]struct {
		version  string
		minMajor uint
		minMinor uint
		want     bool
	}{
		"exact match": {version: "v1.5.0", minMajor: 1, minMinor: 5, want: true},
		"minor above": {version: "v1.6.0", minMajor: 1, minMinor: 5, want: true},
		"minor below": {version: "v1.4.9", minMajor: 1, minMinor: 5, want: false},
		"major above": {version: "v2.0.0", minMajor: 1, minMinor: 5, want: true},
		"major below": {version: "v0.9.9", minMajor: 1, minMinor: 5, want: false},
		"prerelease":  {version: "v1.5.0-pre", minMajor: 1, minMinor: 5, want: true},
		"patch ignored, version patch below min patch": {version: "v1.5.0", minMajor: 1, minMinor: 5, want: true},
		"patch ignored, version patch above min patch": {version: "v1.5.9", minMajor: 1, minMinor: 5, want: true},
		"empty version":   {version: "", minMajor: 1, minMinor: 5, want: false},
		"invalid version": {version: "a", minMajor: 1, minMinor: 5, want: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, AtLeastMajorMinor(tc.version, tc.minMajor, tc.minMinor))
		})
	}
}
