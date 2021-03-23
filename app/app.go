// Copyright (c) 2021 Chair of Applied Cryptography, Technische Universität
// Darmstadt, Germany. All rights reserved. This file is part of
// perun-eth-mobile. Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package app

import (
	"io"

	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"

	ethWallet "perun.network/go-perun/backend/ethereum/wallet"
	"perun.network/go-perun/channel"
	perunio "perun.network/go-perun/pkg/io"
	"perun.network/go-perun/wallet"
)

// PaymentApp represents the app app.
// It implements a channel.App and a channel.StateApp.
type PaymentApp struct {
}

// Def returns the zero address.
func (a *PaymentApp) Def() wallet.Address {
	return ethWallet.AsWalletAddr(common.Address{})
}

// DecodeData decodes the payment data.
func (a *PaymentApp) DecodeData(r io.Reader) (channel.Data, error) {
	var data PaymentData
	return &data, data.Decode(r)
}

// PaymentData serves as the data for the payment app.
type PaymentData struct {
	Invoice [32]byte
	PeerId  string
}

// Decode decodes an Invoice from an io.Reader.
func (d *PaymentData) Decode(r io.Reader) error {
	return perunio.Decode(r, &d.Invoice, &d.PeerId)
}

// Encode encodes an Invoice into an io.Writer.
func (d PaymentData) Encode(w io.Writer) error {
	return perunio.Encode(w, d.Invoice, d.PeerId)
}

// Clone returns a deep copy of an Invoice.
func (d PaymentData) Clone() channel.Data {
	return &d
}

// ValidTransition checks that the data of the `to` state is of type Invoice.
func (a *PaymentApp) ValidTransition(_ *channel.Params, _, to *channel.State, _ channel.Index) error {
	return assertPaymentData(to.Data)
}

// ValidTransition checks that the data of the initial state is of type Invoice.
func (a *PaymentApp) ValidInit(_ *channel.Params, state *channel.State) error {
	return assertPaymentData(state.Data)
}

// assertPaymentData asserts that the given data is of the type PaymentData.
func assertPaymentData(data channel.Data) error {
	if _, ok := data.(*PaymentData); ok {
		return nil
	} else {
		return errors.Errorf("Invalid data type, must be PaymentData, is %T", data)
	}
}
