// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package compat provides utilities for version compatibility checks.
// It contains helpers for decision making based on a client's version for consistent handling
// across our codebase.
package compat

import (
	"fmt"

	"golang.org/x/mod/semver"
)

// AtLeastMajorMinor reports whether version >= "v{minMajor}.{minMinor}" in terms of their major
// and minor version components.
//
// Use it to gate behavior on a minimum client version:
//
//	compat.AtLeastMajorMinor(clientVersion, 1, 36)
//
// minMajor and minMinor should be integer literals so that version gates are easy to find with
// grep or ast-grep.
func AtLeastMajorMinor(version string, minMajor, minMinor uint) bool {
	minVersion := fmt.Sprintf("v%d.%d", minMajor, minMinor)
	return semver.Compare(semver.MajorMinor(version), minVersion) >= 0
}
