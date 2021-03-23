package main

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/perun-network/perun-eth-payment/payment"
)

type Config struct {
	ETHNodeURL string // URL of the ETH node. Example: ws://127.0.0.1:8545
	ChainID    *big.Int
	SK         string
	ServerID   string
	SendAmout  payment.ETHAmount
	Adj, Ah    common.Address

	FundTimeout   time.Duration
	SendTimeout   time.Duration
	CloseTimeout  time.Duration
	DeployTimeout time.Duration

	// maximum amount of funding that the bot will spend to open a channel
	MaxFunding payment.ETHAmount
}
