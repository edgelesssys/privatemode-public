// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package forwarder

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func FuzzMutateAllJSONFields(f *testing.F) {
	f.Add([]byte("{}"))
	f.Add([]byte("[]"))

	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)
		out, err := mutateAllJSONFields(data, noMutation, FieldSelector{})
		require.NoError(err)
		require.NotNil(out)
	})
}

func FuzzMutateSelectJSONFields(f *testing.F) {
	f.Add([]byte("{}"))
	f.Add([]byte("[]"))

	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)
		out, err := mutateSelectJSONFields(data, noMutation, FieldSelector{})
		require.NoError(err)
		require.NotNil(out)
	})
}

// noMutations is a no-op mutation function that returns the input byte slice unchanged.
// As we want to test the JSON parsing and field selection here, not the mutation itself,
// we use this function to ensure that the input data remains unchanged.
func noMutation(in string) (string, error) { return in, nil }
