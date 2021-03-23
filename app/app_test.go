// Copyright (c) 2021 Chair of Applied Cryptography, Technische Universität
// Darmstadt, Germany. All rights reserved. This file is part of
// perun-eth-mobile. Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package app

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/assert"

	"perun.network/go-perun/backend/ethereum/wallet"
	pkgtest "perun.network/go-perun/pkg/test"
)

func TestEncodePaymentData(t *testing.T) {
	rng := pkgtest.Prng(t)

	var app PaymentApp
	assert.Equal(t, app.Def(), &(wallet.Address{}))

	invoice := [32]byte{}

	rng.Read(invoice[:])

	prvKey, _, err := crypto.GenerateKeyPairWithReader(crypto.ECDSA, 2048, rng)
	assert.NoError(t, err)

	// 0.0.0.0 will listen on any interface device.
	sourceMultiAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/5574"))

	// libp2p.New constructs a new libp2p Host.
	host, err := libp2p.New(
		context.Background(),
		libp2p.ListenAddrs(sourceMultiAddr),
		libp2p.Identity(prvKey),
	)
	assert.NoError(t, err)

	peerId := host.ID()

	var data PaymentData
	data = PaymentData{
		Invoice: invoice,
		PeerId:  peerId.Pretty(),
	}

	var buf bytes.Buffer
	err = data.Encode(&buf)
	assert.NoError(t, err)

	var d PaymentData
	err = d.Decode(&buf)
	assert.NoError(t, err)

	assert.Equal(t, d, data)
}
