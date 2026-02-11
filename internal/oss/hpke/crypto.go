// Copyright 2026 Edgeless Systems GmbH. All rights reserved.
// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hpke

import (
	"crypto/ecdh"
	"crypto/mlkem"
)

type Decapsulator interface {
	Encapsulator() Encapsulator
	Decapsulate(ciphertext []byte) (sharedKey []byte, err error)
}

type Encapsulator interface {
	Bytes() []byte
	Encapsulate() (sharedKey, ciphertext []byte)
}

type KeyExchanger interface {
	PublicKey() *ecdh.PublicKey
	Curve() ecdh.Curve
	ECDH(*ecdh.PublicKey) ([]byte, error)
}

type decapsulator768 struct {
	*mlkem.DecapsulationKey768
}

func (d decapsulator768) Encapsulator() Encapsulator {
	return d.EncapsulationKey()
}

func do(f func()) {
	f()
}
