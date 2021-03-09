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

	"github.com/pkg/errors"
	"perun.network/go-perun/channel"
	"perun.network/go-perun/client"
	"perun.network/go-perun/log"
	pkgsync "perun.network/go-perun/pkg/sync"
)

// Channel is a payment channel. Can send and receive funds from a single
// peer. The current balance is available via `Balance()`.
// Payments on a channel cannot happen in both directions at once.
// The same applies to channel closing; one of the participants has to go first.
type Channel struct {
	logger log.Embedding
	closer pkgsync.Closer

	mtx      sync.RWMutex // protects all
	ch       *client.Channel
	received chan *ETHAmount
	client   *Client
	state    *channel.State
}

func newChannel(client *Client, ch *client.Channel) *Channel {
	logger := client.log().WithFields(log.Fields{"id": ch.ID(), "IDx": ch.Idx()})
	channel := &Channel{
		logger:   log.MakeEmbedding(logger),
		ch:       ch,
		received: make(chan *ETHAmount, client.cfg.UpdateBufferSize),
		client:   client,
		state:    ch.State().Clone(),
	}
	go func() {
		// Only log the error case since go-perun logs anyway.
		if err := ch.Watch(channel); err != nil {
			log.WithError(err).Error("Watcher returned an error")
		}
	}()
	return channel
}

// Send sends a payment over the channel. The amount is in Wei.
// Payments cannot be sent simultaneously.
func (c *Channel) Send(ctx context.Context, amount ETHAmount) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.closer.IsClosed() || c.ch.State().IsFinal {
		return errors.New("channel is finalized or closed")
	}

	me := c.ch.Idx()
	if amount.wei.Sign() <= 0 || amount.wei.Cmp(c.state.Balances[assetIdx][me]) > 0 {
		return errors.New("invalid amount")
	}
	var newState *channel.State
	if err := c.ch.UpdateBy(ctx, func(state *channel.State) error {
		transferBal(state.Balances[assetIdx], me, amount)
		// Cloning here is fine, since we do not need the Version.
		newState = state.Clone()
		return nil
	}); err != nil {
		return errors.WithMessage(err, "updating channel")
	}
	// Use cached state to avoid deadlock with update handler.
	c.state = newState
	return nil
}

// transferBal transfers `amount` from `me` to the other participant.
func transferBal(bals []channel.Bal, me channel.Index, amount ETHAmount) {
	bals[me].Sub(bals[me], amount.wei)
	bals[1-me].Add(bals[1-me], amount.wei)
}

// Close gracefully closes the channel. All funds will be distributed on-chain.
func (c *Channel) Close(ctx context.Context) error {
	return c.close(ctx, true)
}

// closes the channel. `remove` indicates whether the channel should be
// removed from the client.
func (c *Channel) close(ctx context.Context, remove bool) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.closer.IsClosed() {
		return nil
	}

	select {
	case <-c.received: // already final?
	default:
		if err := c.ch.UpdateBy(ctx, func(state *channel.State) error {
			state.IsFinal = true
			return nil
		}); err != nil {
			return errors.WithMessage(err, "finalizing channel")
		}
		close(c.received)
	}

	if err := c.ch.Register(ctx); err != nil {
		return errors.WithMessage(err, "registering channel")
	}
	if err := c.ch.Settle(ctx, false); err != nil {
		return errors.WithMessage(err, "settling channel")
	}

	if remove {
		c.client.remChannel(c.state.ID)
	}
	if err := c.ch.Close(); err != nil {
		return errors.WithMessage(err, "closing channel object")
	}
	return c.closer.Close()
}

// Received returns a channel from which all received payments can be read.
// The buffer size can be configured via the config `UpdateBufferSize`.
// It is advised to always read from this channel to avoid blocking client operation.
func (c *Channel) Received() <-chan *ETHAmount {
	return c.received
}

// Balance returns the balance of a channel.
func (c *Channel) Balance() Balance {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	return Balance{
		My:    MakeAmountFromWEI(c.state.Balances[assetIdx][c.ch.Idx()]),
		Other: MakeAmountFromWEI(c.state.Balances[assetIdx][1-c.ch.Idx()]),
	}
}

// HandleAdjudicatorEvent DO NOT CALL.
// It is called by the framework for adjudicator events.
func (c *Channel) HandleAdjudicatorEvent(_e channel.AdjudicatorEvent) {
	if _, ok := _e.(*channel.ConcludedEvent); ok {
		go func() {
			if err := c.Close(c.closer.Ctx()); err != nil {
				c.log().WithError(err).Error("Could not close Channel")
			}
		}()
	}
}

func (c *Channel) handleUpdate(update client.ChannelUpdate, responder *client.UpdateResponder) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	me := c.ch.Idx()
	received := new(big.Int).Sub(update.State.Balances[assetIdx][me], c.state.Balances[assetIdx][me])

	if received.Sign() < 0 {
		c.log().Warnf("Rejecting payment request: %v", received)
		if err := responder.Reject(c.closer.Ctx(), "payment requests are not supported"); err != nil {
			c.log().WithError(err).Error("Failed to reject update")
		}
	} else {
		if err := responder.Accept(c.closer.Ctx()); err != nil {
			c.log().WithError(err).Error("Failed to accept update")
			return
		}

		if received.Sign() > 0 {
			c.log().Infof("Received payment of %v WEI", received)
			c.state = update.State.Clone()
			c.received <- &ETHAmount{received}
		}
		if update.State.IsFinal {
			c.log().Infof("Received final update")
			close(c.received)
		}
	}
}

func (c *Channel) log() log.Logger {
	return c.logger.Log()
}
