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

package payment_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ethtest "perun.network/go-perun/backend/ethereum/wallet/test"
	pkgcontext "perun.network/go-perun/pkg/context"
	pkgtest "perun.network/go-perun/pkg/test"
	"perun.network/go-perun/wire"
	"perun.network/go-perun/wire/net"

	payment "github.com/perun-network/perun-eth-payment"
)

const (
	ipAlice, ipBob = "0.0.0.0:1337", "0.0.0.0:1338"
	skAlice, skBob = "b691bc22c5a30f64876c6136553023d522dcdf0744306dccf4f034a465532e27",
		"b5dc82fc5f4d82b59a38ac963a15eaaedf414f496a037bb4a52310915ac84097"
	chain             = "ws://0.0.0.0:8545"
	rejectReason      = "bob does not like"
	challengeDuration = 60 * time.Second
)

var chainID = big.NewInt(1337)

// These tests need a running eth chain. Example:
// ganache-cli -g 10 -e 1000 -m "pistol kiwi shrug future ozone ostrich match remove crucial oblige cream critic"

type (
	setup struct {
		ctx, cancelled         context.Context
		alice, bob             *payment.Client
		aliceWallet, bobWallet *payment.Wallet
		aliceCh, bobCh         *payment.Channel
		bobPeer, alicePeer     payment.Peer
		// Since `Balance` has `My` and `Other` fields, a balance looks different
		// depending on the party that looks at it. We therefore save the balance
		// twice.
		initBalsAlice, initBalsBob payment.Balance
		adj, ah                    common.Address
	}
	mockedListener struct {
		closed bool
	}
	mockedDialer struct {
		closed bool
	}
)

func newSetup(ctx context.Context, t *testing.T) *setup {
	require := require.New(t)
	aliceWallet, err := payment.NewWalletSK(chainID, skAlice)
	require.NoError(err)
	aliceCfg := payment.MakeConfig(ipAlice, chain, challengeDuration)
	alice, err := payment.NewClient(aliceWallet, aliceCfg)
	require.NoError(err)

	bobWallet, err := payment.NewWalletSK(chainID, skBob)
	require.NoError(err)
	bobCfg := payment.MakeConfig(ipBob, chain, challengeDuration)
	bob, err := payment.NewClient(bobWallet, bobCfg)
	require.NoError(err)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	rng := pkgtest.Prng(t)
	// Alice and Bob start with [10, 90] ETH.
	bobBal, aliceBal := payment.MakeAmountFromETH(rng.Float64()*81+10), payment.MakeAmountFromETH(rng.Float64()*81+10)
	return &setup{
		ctx:       ctx,
		cancelled: cancelled,
		alice:     alice, bob: bob,
		aliceWallet: aliceWallet, bobWallet: bobWallet,
		initBalsAlice: payment.MakeBals(aliceBal, bobBal),
		initBalsBob:   payment.MakeBals(bobBal, aliceBal),
	}
}

func (*mockedListener) Accept() (net.Conn, error) {
	return nil, nil
}

func (l *mockedListener) Close() error {
	l.closed = true
	return nil
}

func (*mockedDialer) Dial(context.Context, wire.Address) (net.Conn, error) {
	return nil, nil
}

func (*mockedDialer) Register(wire.Address, string) {}

func (d *mockedDialer) Close() error {
	d.closed = true
	return nil
}

func TestWallet(t *testing.T) {
	// Create Secret key wallet.
	a, err := payment.NewWalletSK(big.NewInt(2), "cc5b26585ce561588e0bc100dc92fa77aa86b156ba8e200a8dee588e323928b9")
	require.NoError(t, err)
	// Create Mnemonic wallet.
	b, err := payment.NewWalletHD(big.NewInt(1), "pistol kiwi shrug future ozone ostrich match remove crucial oblige cream critic", 5)
	require.NoError(t, err)
	// Assert that they produce the same address.
	assert.True(t, a.Address.Equals(b.Address))
}

func TestNewClient(t *testing.T) {
	wallet, err := payment.NewWalletSK(chainID, skAlice)
	require.NoError(t, err)

	cfg := payment.MakeConfig(ipAlice, chain, challengeDuration)
	_, err = payment.NewClient(nil, cfg)
	assert.Error(t, err)

	cfg = payment.MakeConfig("123", chain, challengeDuration)
	_, err = payment.NewClient(wallet, cfg)
	assert.Error(t, err)

	cfg = payment.MakeConfig(ipAlice, "123", challengeDuration)
	_, err = payment.NewClient(wallet, cfg)
	assert.Error(t, err)

	cfg = payment.MakeConfig(ipAlice, chain, 0)
	_, err = payment.NewClient(wallet, cfg)
	assert.Error(t, err)
}

func TestNewClient_Options(t *testing.T) {
	wallet, err := payment.NewWalletSK(chainID, skAlice)
	require.NoError(t, err)
	cfg := payment.MakeConfig(ipAlice, chain, challengeDuration)

	var dialer mockedDialer
	var listener mockedListener
	client, err := payment.NewClient(wallet, cfg, payment.WithDialer(&dialer), payment.WithListener(&listener))

	require.NoError(t, err)
	assert.False(t, dialer.closed)
	assert.False(t, listener.closed)
	assert.NoError(t, client.Close(context.Background()))
	assert.True(t, dialer.closed)
	assert.True(t, listener.closed)
}

func TestAliceBob(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := newSetup(ctx, t)
	// Store on-chain balances.
	balsBefore, err := s.alice.OnChainBalance(s.ctx, s.aliceWallet.Address, s.bobWallet.Address)
	require.NoError(t, err)

	testRegisterPeer(s, t)
	testContracts(s, t)
	// Open, Send, Close.
	testOpen(s, t)
	testSend(s, t)
	testCloseChannel(s, t)
	// Test that the channel got closed.
	testChannelClosed(s, t)
	// Open, Send, Close.
	// This is done twice to cover the that the channel is closed by the client.
	testOpen(s, t)
	testSend(s, t)
	// Close the client.
	testCloseClient(s, t)
	// Test that channel+client got closed.
	testChannelClosed(s, t)
	testClientClosed(s, t)
	// Check that on-chain balances of Alice and Bob only changed by one milli
	// Eth which was used as Gas. The balances should otherwise stay the same
	// since Bob always echoes back Alice's payments.
	mEth := new(big.Int).Exp(big.NewInt(10), big.NewInt(15), nil)
	assertBalancesDelta(t, s, balsBefore, mEth)
}

func testRegisterPeer(s *setup, t *testing.T) {
	var err error
	require := require.New(t)
	// Normal register.
	s.bobPeer, err = s.alice.RegisterPeer(s.bobWallet.Address, ipBob, "bob")
	require.NoError(err)
	s.alicePeer, err = s.bob.RegisterPeer(s.aliceWallet.Address, ipAlice, "alice")
	require.NoError(err)
	// Double register.
	_, err = s.bob.RegisterPeer(s.aliceWallet.Address, ipAlice, "alice")
	require.Error(err)
	// Double register of address with different alias.
	_, err = s.bob.RegisterPeer(s.aliceWallet.Address, ipAlice, "alice2")
	require.Error(err)
	// Own register.
	_, err = s.bob.RegisterPeer(s.bobWallet.Address, ipBob, "bob")
	require.Error(err)
	// Empty alias register.
	_, err = s.bob.RegisterPeer(s.bobWallet.Address, ipBob, "")
	require.Error(err)
}

func testContracts(s *setup, t *testing.T) {
	require := require.New(t)

	// Deploy contracts with cancelled context.
	_, _, err := payment.DeployContracts(s.cancelled, s.aliceWallet, chain)
	require.Error(err)
	// Deploy contracts.
	s.adj, s.ah, err = payment.DeployContracts(s.ctx, s.aliceWallet, chain)
	require.NoError(err)
	// Set contracts with cancelled context.
	err = s.bob.Init(s.cancelled, s.ah, s.adj)
	require.True(pkgcontext.IsContextError(err))
	// Set contracts with invalid address.
	require.Error(s.bob.Init(s.ctx, s.ah, s.adj))
	// Set contracts.
	require.NoError(s.alice.Init(s.ctx, s.adj, s.ah))
	require.NoError(s.bob.Init(s.ctx, s.adj, s.ah))
	// Set contracts twice.
	require.Error(s.bob.Init(s.ctx, s.adj, s.ah))
}

func testOpen(s *setup, t *testing.T) {
	ct := pkgtest.NewConcurrent(t)

	go ct.Stage("alice", func(t pkgtest.ConcT) {
		require := require.New(t)
		// Invalid alias should fail.
		_, err := s.alice.ProposeChannel(s.ctx, payment.Peer{}, s.initBalsAlice)
		require.Error(err)
		// Negative balances in propsal should fail.
		neg := payment.MakeBals(payment.MakeAmountFromETH(-100), payment.MakeAmountFromETH(-100))
		_, err = s.alice.ProposeChannel(s.ctx, s.bobPeer, neg)
		require.Error(err)
		// First propsal will be rejected.
		_, err = s.alice.ProposeChannel(s.ctx, s.bobPeer, s.initBalsAlice)
		require.Error(err)
		// Second proposal will be accepted.
		channel, err := s.alice.ProposeChannel(s.ctx, s.bobPeer, s.initBalsAlice)
		require.NoError(err)
		s.aliceCh = channel
	})
	go ct.Stage("bob", func(t pkgtest.ConcT) {
		require := require.New(t)
		// Reject first channel.
		prop := <-s.bob.Proposals()
		require.Equal(prop.Balance, s.initBalsBob)
		require.Equal(prop.From, s.alicePeer)
		testProposalResponse(s, prop, false, t)
		// Accept second channel.
		prop = <-s.bob.Proposals()
		require.Equal(prop.Balance, s.initBalsBob)
		require.Equal(prop.From, s.alicePeer)
		s.bobCh = testProposalResponse(s, prop, true, t)
	})

	ct.Wait("alice", "bob")
}

func testProposalResponse(s *setup, prop *payment.ChannelProposal, accept bool, t require.TestingT) (channel *payment.Channel) {
	var err error
	require := require.New(t)
	// TODO https://github.com/hyperledger-labs/go-perun/issues/25
	if accept {
		channel, err = prop.Accept(s.ctx)
		require.NoError(err)
	} else {
		require.NoError(prop.Reject(s.ctx, rejectReason))
	}
	// Accept and Reject cannot be called twice.
	_, err = prop.Accept(s.ctx)
	require.Error(err)
	require.Error(prop.Reject(s.ctx, ""))
	return
}

func testSend(s *setup, t *testing.T) {
	ct := pkgtest.NewConcurrent(t)

	go ct.Stage("alice", func(ct pkgtest.ConcT) {
		require := require.New(ct)
		// Send negative.
		require.Error(s.aliceCh.Send(s.ctx, payment.MakeAmountFromETH(-1)))
		// Send context cancelled.
		require.Error(s.aliceCh.Send(s.cancelled, payment.MakeAmountFromETH(1)))
		// Send too much.
		require.Error(s.aliceCh.Send(s.ctx, payment.MakeAmountFromETH(200)))
		// Send a random amount of [1, 10] ETH.
		rng := pkgtest.Prng(t)
		for i := 0; i < 5; i++ {
			amount := payment.MakeAmountFromETH(rng.Float64()*10 + 1)
			require.NoError(s.aliceCh.Send(s.ctx, amount))
			received := <-s.aliceCh.Received()
			require.Equal(amount, *received)
		}
	})
	go ct.Stage("bob", func(t pkgtest.ConcT) {
		require := require.New(t)
		// Accept normal.
		for i := 0; i < 5; i++ {
			received := <-s.bobCh.Received()
			require.NoError(s.bobCh.Send(s.ctx, *received))
		}
	})

	ct.Wait("alice", "bob")
}

func testCloseChannel(s *setup, t *testing.T) {
	ct := pkgtest.NewConcurrent(t)

	go ct.Stage("alice", func(t pkgtest.ConcT) {
		require.NoError(t, s.aliceCh.Close(s.ctx))
	})
	go ct.Stage("bob", func(t pkgtest.ConcT) {
		// Wait 1s since they cannot be closed simultaneously.
		// Alice and Bob would otherwise send each other a final TX at the same
		// time, leading to a deadlock.
		time.Sleep(1 * time.Second)
		require.NoError(t, s.bobCh.Close(s.ctx))
	})

	ct.Wait("alice", "bob")
}

func testChannelClosed(s *setup, t *testing.T) {
	require := require.New(t)

	// Closing twice is fine.
	require.NoError(s.aliceCh.Close(s.ctx))
	require.NoError(s.bobCh.Close(s.ctx))
	// Sending on a closed channel errors.
	require.Error(s.aliceCh.Send(s.ctx, payment.MakeAmountFromETH(1)))
	require.Error(s.bobCh.Send(s.ctx, payment.MakeAmountFromETH(1)))
	// The `Received` channel is closed.
	require.Nil(<-s.aliceCh.Received())
	require.Nil(<-s.bobCh.Received())
	// Check that both channels have the same balance.
	require.Equal(s.aliceCh.Balance().My, s.bobCh.Balance().Other)
	require.Equal(s.aliceCh.Balance().Other, s.bobCh.Balance().My)
}

func testCloseClient(s *setup, t *testing.T) {
	ct := pkgtest.NewConcurrent(t)

	go ct.Stage("alice", func(t pkgtest.ConcT) {
		require.NoError(t, s.alice.Close(s.ctx))
	})
	go ct.Stage("bob", func(t pkgtest.ConcT) {
		// Wait 1s since they cannot be closed simultaneously.
		// Alice and Bob would otherwise send each other a final TX at the same
		// time, leading to a deadlock.
		time.Sleep(1 * time.Second)
		require.NoError(t, s.bob.Close(s.ctx))
	})

	ct.Wait("alice", "bob")
}

func testClientClosed(s *setup, t *testing.T) {
	require := require.New(t)

	// Closing twice is fine.
	require.NoError(s.alice.Close(s.ctx))
	require.NoError(s.bob.Close(s.ctx))
	// Register peer fails on closed client.
	rng := pkgtest.Prng(t)
	addr := ethtest.NewRandomAddress(rng)
	_, err := s.alice.RegisterPeer(&addr, ipBob, "charlie")
	require.Error(err)
	_, err = s.bob.RegisterPeer(&addr, ipBob, "charlie")
	require.Error(err)
	// Proposing fails on closed client.
	_, err = s.alice.ProposeChannel(s.ctx, s.bobPeer, s.initBalsAlice)
	require.Error(err)
	_, err = s.bob.ProposeChannel(s.ctx, s.alicePeer, s.initBalsBob)
	require.Error(err)
	// Init fails on closed client.
	err = s.alice.Init(s.ctx, s.adj, s.ah)
	require.Error(err)
	err = s.bob.Init(s.ctx, s.adj, s.ah)
	require.Error(err)
	// Proposal channels are closed.
	<-s.alice.Proposals()
	<-s.bob.Proposals()
}

// assertBalancesDelta checks that Alice' and Bobs balances only differ
// `delta` from `bals`.
func assertBalancesDelta(t *testing.T, s *setup, bals []*big.Int, delta *big.Int) {
	got, err := s.bob.OnChainBalance(s.ctx, s.aliceWallet.Address, s.bobWallet.Address)
	require.NoError(t, err)

	diffBob := new(big.Int).Sub(bals[0], got[0])
	assert.LessOrEqual(t, diffBob.CmpAbs(delta), 0)
	diffAlice := new(big.Int).Sub(bals[1], got[1])
	assert.LessOrEqual(t, diffAlice.CmpAbs(delta), 0)
}
