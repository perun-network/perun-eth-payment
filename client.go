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
	"context"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/pkg/errors"
	"perun.network/go-perun/apps/payment"
	ethchannel "perun.network/go-perun/backend/ethereum/channel"
	ethwallet "perun.network/go-perun/backend/ethereum/wallet"
	"perun.network/go-perun/channel"
	"perun.network/go-perun/client"
	"perun.network/go-perun/log"
	perunerrors "perun.network/go-perun/pkg/errors"
	pkgsync "perun.network/go-perun/pkg/sync"
	"perun.network/go-perun/wallet"
	"perun.network/go-perun/wire"
	"perun.network/go-perun/wire/net"
	"perun.network/go-perun/wire/net/simple"
)

// The Client is the central object for opening payment channels.
//
// It is completely thread safe and should not be copied.
// Always `Close()` a client when your application shuts down to avoid
// losing funds.
type Client struct {
	logger log.Embedding
	closer pkgsync.Closer
	cfg    Config

	mtx   sync.Mutex   // protects all except channels
	chMtx sync.RWMutex // protects channels

	wallet *Wallet
	chain  *ethclient.Client
	cb     ethchannel.ContractBackend
	adj    *ethchannel.Adjudicator
	ah     common.Address
	funder channel.Funder
	perun  *client.Client

	dialer   Dialer
	listener net.Listener
	bus      *net.Bus
	peers    map[common.Address]Peer

	proposals chan *ChannelProposal
	channels  map[channel.ID]*Channel
}

// NewClient creates a new client.
// The account from `wallet` will be used to send on-chain transactions.
// Pass one or more `ClientOption`s to modify the default behaviour.
func NewClient(wallet *Wallet, cfg Config, opts ...ClientOption) (*Client, error) {
	if uint64(cfg.ChallengeDuration) == 0 {
		return nil, errors.New("invalid challenge duration")
	}
	if wallet == nil {
		return nil, errors.New("wallet pointer nil")
	}
	// Connect to chain.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.DialTimeout)
	defer cancel()
	chain, err := ethclient.DialContext(ctx, cfg.ChainURL)
	if err != nil {
		return nil, errors.WithMessage(err, "connecting to ethereum node")
	}
	cb := ethchannel.NewContractBackend(chain, wallet.transactor)
	client := &Client{
		logger:    log.MakeEmbedding(log.WithField("role", "client")),
		cfg:       cfg,
		wallet:    wallet,
		chain:     chain,
		cb:        cb,
		peers:     make(map[common.Address]Peer),
		proposals: make(chan *ChannelProposal, cfg.ProposalBufferSize),
		channels:  make(map[channel.ID]*Channel),
	}
	// Setup Dialer and Listener with respect to the passed options.
	for _, opt := range opts {
		opt(client)
	}
	if client.dialer == nil {
		client.dialer = simple.NewTCPDialer(cfg.DialTimeout)
	}
	if client.listener == nil {
		client.listener, err = simple.NewTCPListener(cfg.Host)
		if err != nil {
			return nil, errors.WithMessage(err, "creating listener")
		}
	}
	client.bus = net.NewBus(wallet.wAcc, client.dialer)
	return client, nil
}

// Init sets and verifies the addresses of the Adjudicator and
// Assetholder contracts.
func (c *Client) Init(ctx context.Context, adj, ah common.Address) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.closer.IsClosed() {
		return errors.New("closed")
	}
	if c.adj != nil || c.ah != (common.Address{}) {
		return errors.New("contracts already set")
	}
	// Assetholder validation includes Adjudicator validation.
	if err := ethchannel.ValidateAssetHolderETH(ctx, c.cb, ah, adj); err != nil {
		return errors.WithMessage(err, "validating contracts")
	}
	c.adj = ethchannel.NewAdjudicator(c.cb, adj, c.wallet.acc.Address, c.wallet.acc)
	c.ah = ah
	ethDepositor := new(ethchannel.ETHDepositor)
	accounts := map[ethchannel.Asset]accounts.Account{ethwallet.Address(c.ah): c.wallet.acc}
	depositors := map[ethchannel.Asset]ethchannel.Depositor{ethwallet.Address(c.ah): ethDepositor}
	c.funder = ethchannel.NewFunder(c.cb, accounts, depositors)

	var err error
	c.perun, err = client.New(c.wallet.Address, c.bus, c.funder, c.adj, c.wallet.wallet)
	if err != nil {
		return errors.WithMessage(err, "creating perun client")
	}
	c.perun.OnNewChannel(c.handleNewChannel)
	go c.perun.Handle(c, c)
	go c.bus.Listen(c.listener)
	return nil
}

// OnChainBalance returns the on-chain balances for the passed addresses in WEI.
func (c *Client) OnChainBalance(ctx context.Context, addrs ...wallet.Address) ([]*big.Int, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	var err error
	balances := make([]*big.Int, len(addrs))

	for i, addr := range addrs {
		balances[i], err = c.chain.BalanceAt(ctx, ethwallet.AsEthAddr(addr), nil)
		if err != nil {
			return nil, err
		}
	}
	return balances, nil
}

// RegisterPeer registers a peer in the client. This is necessary to be able to
// send or receive a channel proposal from a peer.
// `addr` is the peers address.
// `host` is the ip:port that the peer listens on.
// `alias` a nickname for the peer. Does not need to be unique.
func (c *Client) RegisterPeer(addr wire.Address, host, alias string) (Peer, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.closer.IsClosed() {
		return Peer{}, errors.New("closed")
	}
	if alias == "" {
		return Peer{}, errors.New("alias cannot be empty")
	}
	if addr.Equals(c.wallet.Address) {
		return Peer{}, errors.New("cannot register yourself")
	}
	ethAddr := ethwallet.AsEthAddr(addr)
	if _, ok := c.peers[ethAddr]; ok {
		return Peer{}, errors.Errorf("alias already registered: %s", alias)
	}

	peer := Peer{ethAddr, alias}
	c.peers[ethAddr] = peer
	c.dialer.Register(addr, host)
	return peer, nil
}

// ProposeChannel proposes a channel to `peer`.
// `bals` is the initial balance of the channel. It must be lower than the
// on-chain balance since both participants will use their on-chain balance
// to fund the channel.
func (c *Client) ProposeChannel(ctx context.Context, peer Peer, bals Balance) (*Channel, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.closer.IsClosed() {
		return nil, errors.New("closed")
	}
	peer, ok := c.peers[peer.addr]
	if !ok {
		return nil, errors.Errorf("unknown peer '%s'", peer.alias)
	}
	if c.perun == nil {
		return nil, errors.Errorf("client nil")
	}

	// Perun needs an initial allocation which defines the balances of all
	// participants. The same structure is used for multi-asset channels.
	initBals := &channel.Allocation{
		Assets:   []channel.Asset{ethwallet.AsWalletAddr(c.ah)},
		Balances: [][]*big.Int{{bals.My.wei, bals.Other.wei}},
	}
	// The addresses of the channel participants, i.e., our address and the
	// peer's address. We use the same accounts for on-chain and off-chain,
	// but we could use different ones.
	peers := []wire.Address{
		c.wallet.Address,
		ethwallet.AsWalletAddr(peer.addr),
	}
	// Prepare the proposal by defining the channel parameters.
	proposal, err := client.NewLedgerChannelProposal(c.cfg.challengeDurationSec(), c.wallet.Address, initBals, peers, client.WithApp(app, payment.Data()))
	if err != nil {
		return nil, errors.WithMessage(err, "creating channel proposal")
	}
	// Send the proposal.
	_channel, err := c.perun.ProposeChannel(ctx, proposal)
	if err != nil {
		return nil, errors.WithMessage(err, "proposing channel")
	}
	return c.addChannel(_channel), nil
}

// Proposals returns a channel that all incoming channel proposals can be read
// from. The buffer size can be configured via the config `ProposalBufferSize`.
// It is advised to always read from this channel to avoid blocking client operation.
func (c *Client) Proposals() <-chan *ChannelProposal {
	return c.proposals
}

// Close gracefully closes the Client and all open Channels.
func (c *Client) Close(ctx context.Context) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.closer.IsClosed() {
		return nil
	}
	c.chMtx.RLock()
	defer c.chMtx.RUnlock()

	var clientErr error
	// It should be possible to close a client before calling `Init()`.
	if c.perun != nil {
		errG := perunerrors.NewGatherer()
		for id, ch := range c.channels {
			ch := ch
			c.log().WithField("id", id).Debug("Closing channel")
			errG.Go(func() error {
				return ch.close(ctx, false)
			})
		}
		if err := errG.Wait(); err != nil {
			return err
		}
		if err := c.closer.Close(); err != nil {
			c.log().WithError(err).Error("Could not close Closer")
		}
		close(c.proposals)
		clientErr = c.perun.Close()
	}
	if err := c.dialer.Close(); err != nil {
		c.log().WithError(err).Error("Could not close Dialer")
	}
	if err := c.listener.Close(); err != nil {
		c.log().WithError(err).Error("Could not close Listener")
	}
	return errors.WithMessage(clientErr, "closing go-perun client")
}

func (c *Client) handleNewChannel(ch *client.Channel) {
	c.log().WithField("id", ch.ID()).Debug("HandleNewChannel")
}

// HandleProposal DO NOT CALL.
// It is called by the framework for incoming channel proposals.
func (c *Client) HandleProposal(_prop client.ChannelProposal, responder *client.ProposalResponder) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	prop, from, err := c.isValid(_prop)
	if err != nil {
		c.log().WithError(err).Warn("Received invalid channel proposal")
		err = responder.Reject(c.closer.Ctx(), err.Error())
		c.log().WithError(err).Warn("Rejecting channel")
		return
	}

	c.proposals <- newChannelProposal(c, from, prop, responder)
}

// HandleUpdate DO NOT CALL.
// It is called by the framework for incoming channel updates.
func (c *Client) HandleUpdate(update client.ChannelUpdate, responder *client.UpdateResponder) {
	ch, ok := c.getChannel(update.State.ID)
	if !ok {
		c.log().WithField("id", update.State.ID).Error("Received update for unknown channel")
	}
	ch.handleUpdate(update, responder)
}

func (c *Client) isValid(_prop client.ChannelProposal) (*client.LedgerChannelProposal, common.Address, error) {
	if c.ah == (common.Address{}) {
		return nil, common.Address{}, errors.New("asset holder contract not set")
	}
	prop, ok := _prop.(*client.LedgerChannelProposal)
	if !ok {
		return nil, common.Address{}, errors.New("not a ledger channel")
	}
	from, ok := prop.Participant.(*ethwallet.Address)
	if !ok {
		return nil, common.Address{}, errors.New("wrong address type")
	}
	if _, ok := prop.App.(*payment.App); !ok {
		return nil, common.Address{}, errors.New("wrong app type")
	}
	if !prop.App.Def().Equals(app.Def()) {
		return nil, common.Address{}, errors.New("wrong app def")
	}
	if !payment.IsData(prop.InitData) {
		return nil, common.Address{}, errors.New("wrong init data")
	}
	if len(prop.InitBals.Assets) != 1 {
		return nil, common.Address{}, errors.New("more than one asset")
	}
	asset, ok := prop.InitBals.Assets[assetIdx].(*ethchannel.Asset)
	if !ok {
		return nil, common.Address{}, errors.New("wrong asset type")
	}
	if !asset.Equals(ethwallet.AsWalletAddr(c.ah)) {
		return nil, common.Address{}, errors.New("wrong asset address")
	}
	if !prop.FundingAgreement.Equal(prop.InitBals.Balances) {
		return nil, common.Address{}, errors.New("funding agreement does not match init bals")
	}
	if prop.ChallengeDuration != c.cfg.challengeDurationSec() {
		return nil, common.Address{}, errors.New("wrong challenge duration")
	}
	if len(prop.Peers) != 2 {
		return nil, common.Address{}, errors.New("not a two party channel")
	}
	return prop, common.Address(*from), nil
}

func (c *Client) addChannel(_ch *client.Channel) *Channel {
	c.chMtx.Lock()
	defer c.chMtx.Unlock()

	ch := newChannel(c, _ch)
	c.channels[_ch.ID()] = ch
	return ch
}

func (c *Client) getChannel(id channel.ID) (ch *Channel, ok bool) {
	c.chMtx.RLock()
	defer c.chMtx.RUnlock()

	ch, ok = c.channels[id]
	return
}

func (c *Client) remChannel(id channel.ID) {
	c.chMtx.Lock()
	defer c.chMtx.Unlock()

	delete(c.channels, id)
}

func (c *Client) log() log.Logger {
	return c.logger.Log()
}
