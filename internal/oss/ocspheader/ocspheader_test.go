// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package ocspheader

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeader(t *testing.T) {
	t.Run("MarshalUnmarshal", func(t *testing.T) {
		require := require.New(t)
		assert := assert.New(t)

		secret := getSecret(t)

		allowedStatuses := []AllowStatus{AllowStatusGood, AllowStatusRevoked}
		header := NewHeader(allowedStatuses, time.Now().Round(time.Second))
		marshaled, err := header.Marshal()
		require.NoError(err)
		assert.NotEmpty(marshaled)
		marshaledMAC, err := header.MarshalMACHeader(secret)
		require.NoError(err)
		assert.NotEmpty(marshaledMAC)

		unmarshaledAllowedStatus, err := UnmarshalAndVerify(marshaled, marshaledMAC, secret)
		require.NoError(err)
		assert.Equal(unmarshaledAllowedStatus.AllowedStatuses, allowedStatuses)
		assert.Equal(unmarshaledAllowedStatus.RevokedNbf, header.RevokedNbf)
	})

	t.Run("MarshalNoStatus", func(t *testing.T) {
		require := require.New(t)

		header := NewHeader([]AllowStatus{}, time.Time{})
		_, err := header.Marshal()
		require.Error(err)
	})

	t.Run("UnmarshalInvalidMAC", func(t *testing.T) {
		require := require.New(t)
		assert := assert.New(t)

		secret := getSecret(t)

		allowedStatuses := []AllowStatus{AllowStatusGood, AllowStatusUnknown, AllowStatusRevoked}
		header := NewHeader(allowedStatuses, time.Time{})
		marshaled, err := header.Marshal()
		require.NoError(err)
		assert.NotEmpty(marshaled)
		marshaledMAC := "invalid-mac"

		_, err = UnmarshalAndVerify(marshaled, marshaledMAC, secret)
		require.Error(err)
	})
}

func getSecret(t *testing.T) [32]byte {
	t.Helper()
	require := require.New(t)

	var secret [32]byte

	n, err := rand.Read(secret[:])
	require.NoError(err)
	require.Equal(32, n)

	return secret
}
