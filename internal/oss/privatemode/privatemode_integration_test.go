// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

//go:build integration

package privatemode

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchManifest(t *testing.T) {
	require := require.New(t)

	client := New("") // Manifest fetching should work without an API key
	manifest, err := client.FetchManifest(t.Context())
	require.NoError(err)
	require.NotEmpty(manifest)
}
