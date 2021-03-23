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

// Package payment contains a simplified API of go-perun.
package payment

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"perun.network/go-perun/apps/payment"
	ethchannel "perun.network/go-perun/backend/ethereum/channel"
	ethwallet "perun.network/go-perun/backend/ethereum/wallet"
	"perun.network/go-perun/channel"
	perunlogrus "perun.network/go-perun/log/logrus"
)

type (
	// Peer identifies a known peer. Can only be created with Client.AddPeer.
	Peer struct {
		addr  common.Address
		alias string
	}

	// ETHAmount represents an amount of Ethereum.
	ETHAmount struct {
		wei *big.Int
	}

	// Balance represents the balance of funds in a channel.
	Balance struct {
		My, Other ETHAmount
	}
)

func (p *Peer) Addr() string {
	return p.addr.String()
}

var app = &payment.App{Addr: &ethwallet.Address{}}

// MakeAmountFromETH creates an `ETHAmount` with the given amount of ethereum set.
// Floating-point imprecision may apply.
func MakeAmountFromETH(eth float64) ETHAmount {
	wei, _ := new(big.Float).Mul(big.NewFloat(eth), big.NewFloat(params.Ether)).Int(nil)
	return ETHAmount{wei}
}

// MakeAmountFromWEI creates an `ETHAmount` with the given amount of WEI set.
func MakeAmountFromWEI(wei *big.Int) ETHAmount {
	if wei == nil {
		panic("wei cannot be nil")
	}
	return ETHAmount{wei}
}

// ETH returns the amount in Ethereum.
// Floating-point imprecision may apply.
func (a ETHAmount) ETH() float64 {
	eth, _ := new(big.Float).Quo(new(big.Float).SetInt(a.wei), big.NewFloat(params.Ether)).
		Float64()
	return eth
}

// WEI returns the amount in WEI.
func (a ETHAmount) WEI() *big.Int {
	return a.wei
}

// MakeBals creates a new Balance struct containing the given balances.
func MakeBals(my, other ETHAmount) Balance {
	return Balance{
		My:    my,
		Other: other,
	}
}

// DeployContracts deploys the Adjudicator and Assetholder contract and returns
// their addresses.
// The context timeout must be at least twice the block time to allow for the
// transactions to be mined. To be safe, put it higher.
func DeployContracts(ctx context.Context, wallet *Wallet, chainURL string) (common.Address, common.Address, error) {
	chain, err := ethclient.DialContext(ctx, chainURL)
	if err != nil {
		return common.Address{}, common.Address{}, errors.WithMessage(err, "connecting to ethereum node")
	}
	cb := ethchannel.NewContractBackend(chain, wallet.transactor)

	adj, err := ethchannel.DeployAdjudicator(ctx, cb, wallet.acc)
	if err != nil {
		return common.Address{}, common.Address{}, errors.WithMessage(err, "contracts already set")
	}
	ah, err := ethchannel.DeployETHAssetholder(ctx, cb, adj, wallet.acc)
	if err != nil {
		return common.Address{}, common.Address{}, errors.WithMessage(err, "deploying assetholder")
	}
	return adj, ah, errors.WithMessage(err, "initializing peer")
}

func SetApp(app channel.App) {
	channel.RegisterApp(app)
}

// SetLogLevel can be used to define how verbose the log output should be.
// The default value is `WarnLevel` with disabled console colors.
// Set it to `Debug` or even `Trace` if you are debugging an error.
// Example: `SetLogLevel(logrus.DebugLevel, &logrus.TextFormatter{})`.
func SetLogLevel(level logrus.Level, formatter logrus.Formatter) {
	perunlogrus.Set(level, formatter)
}

func init() {
	perunlogrus.Set(logrus.WarnLevel, &logrus.TextFormatter{ForceColors: false})
	channel.RegisterApp(app)
}
