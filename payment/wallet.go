// Copyright 2021 PolyCrypt GmbH
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package payment

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	hdwallet "github.com/miguelmota/go-ethereum-hdwallet"
	"github.com/pkg/errors"
	ethchannel "perun.network/go-perun/backend/ethereum/channel"
	ethwallet "perun.network/go-perun/backend/ethereum/wallet"
	perunhd "perun.network/go-perun/backend/ethereum/wallet/hd"
	perunsimple "perun.network/go-perun/backend/ethereum/wallet/simple"
	"perun.network/go-perun/wallet"
)

// Wallet contains a single account that will be used to send on-chain
// transactions. Its address is public.
type Wallet struct {
	wallet     wallet.Wallet
	transactor ethchannel.Transactor
	acc        accounts.Account
	wAcc       wallet.Account

	Address wallet.Address
}

// NewWalletHD creates a new single-account HD-wallet with the given mnemonic.
// `index` is the index of the account that will be used.
func NewWalletHD(chainID *big.Int, mnemonic string, index uint) (*Wallet, error) {
	// Create wallet.
	rootWallet, err := hdwallet.NewFromMnemonic(mnemonic)
	if err != nil {
		return nil, errors.WithMessage(err, "parsing secret key")
	}
	wallet, err := perunhd.NewWallet(rootWallet, "m/44'/60'/0'/0/0", index)
	if err != nil {
		return nil, fmt.Errorf("deriving path: %w", err)
	}
	// Derive the first account from the wallet.
	acc, err := wallet.NewAccount()
	if err != nil {
		return nil, fmt.Errorf("deriving hd account: %w", err)
	}
	// Create transactor.
	signer := types.NewEIP155Signer(chainID)
	transactor := perunhd.NewTransactor(wallet.Wallet(), signer)

	return &Wallet{
		wallet:     wallet,
		transactor: transactor,
		acc:        acc.Account,
		Address:    acc.Address(),
		wAcc:       acc,
	}, nil
}

// NewWalletSK creates a new single-account wallet from a secret key.
func NewWalletSK(chainID *big.Int, sk string) (*Wallet, error) {
	key, err := crypto.HexToECDSA(sk)
	if err != nil {
		return nil, errors.WithMessage(err, "parsing secret key")
	}
	// Create wallet.
	wallet := perunsimple.NewWallet(key)
	if err != nil {
		return nil, errors.WithMessage(err, "creating wallet")
	}
	// Unlock account.
	addr := crypto.PubkeyToAddress(key.PublicKey)
	acc, err := wallet.Unlock(ethwallet.AsWalletAddr(addr))
	if err != nil {
		return nil, errors.WithMessage(err, "creating account")
	}
	// Create transactor.
	signer := types.NewEIP155Signer(chainID)
	transactor := perunsimple.NewTransactor(wallet, signer)

	return &Wallet{
		wallet:     wallet,
		transactor: transactor,
		acc:        acc.(*perunsimple.Account).Account,
		Address:    acc.Address(),
		wAcc:       acc,
	}, nil
}
