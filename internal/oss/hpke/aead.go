// Copyright 2026 Edgeless Systems GmbH. All rights reserved.
// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hpke

import (
	"crypto/cipher"
)

// The AEAD is one of the three components of an HPKE ciphersuite, implementing
// symmetric encryption.
type AEAD interface {
	ID() uint16
	keySize() int
	nonceSize() int
	aead(key []byte) (cipher.AEAD, error)
}

// ExportOnly returns a placeholder AEAD implementation that cannot encrypt or
// decrypt, but only export secrets with [Sender.Export] or [Recipient.Export].
//
// When this is used, [Sender.Seal] and [Recipient.Open] return errors.
func ExportOnly() AEAD { return exportOnlyAEAD{} }

type exportOnlyAEAD struct{}

func (exportOnlyAEAD) ID() uint16 {
	return 0xFFFF
}

func (exportOnlyAEAD) aead(key []byte) (cipher.AEAD, error) {
	return nil, nil
}

func (exportOnlyAEAD) keySize() int {
	return 0
}

func (exportOnlyAEAD) nonceSize() int {
	return 0
}
