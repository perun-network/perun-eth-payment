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

	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"perun.network/go-perun/client"
	pkgatomic "perun.network/go-perun/pkg/sync/atomic"
)

// ChannelProposal models a proposal for a channel that was received
// by the Client from a known Peer.
// Can be accepted or rejected. Fully thread-safe. Should not be copied.
type ChannelProposal struct {
	called    pkgatomic.Bool
	client    *Client
	prop      *client.LedgerChannelProposal
	responder *client.ProposalResponder
	From      common.Address // Address that sent it.
	Balance   Balance        // Initial balance of the channel.
}

func newChannelProposal(client *Client, from common.Address, prop *client.LedgerChannelProposal, responder *client.ProposalResponder) *ChannelProposal {
	// The proposer always has index 0 and we are not the proposer.
	myBal := MakeAmountFromWEI(prop.InitBals.Balances[assetIdx][1])
	otherBal := MakeAmountFromWEI(prop.InitBals.Balances[assetIdx][0])

	return &ChannelProposal{
		client:    client,
		prop:      prop,
		responder: responder,
		From:      from,
		Balance:   MakeBals(myBal, otherBal),
	}
}

// Accept accepts the channel proposal.
// Errors if the proposal was already accepted or rejected.
//
// It is important that the passed context does not cancel before twice the
// block time has passed (at least for real blockchain backends with wall
// time), or the channel cannot be settled if a peer times out funding.
func (p *ChannelProposal) Accept(ctx context.Context) (*Channel, error) {
	if !p.called.TrySet() {
		return nil, errors.New("proposal already accepted or rejected")
	}
	accept := p.prop.Accept(p.client.wallet.Address, client.WithRandomNonce())
	ch, err := p.responder.Accept(ctx, accept)
	if err != nil {
		return nil, errors.WithMessage(err, "accepting channel proposal")
	}
	return p.client.addChannel(ch), nil
}

// Reject rejects the channel proposal with a reason.
// Errors if the proposal was already accepted or rejected.
func (p *ChannelProposal) Reject(ctx context.Context, reason string) error {
	if !p.called.TrySet() {
		return errors.New("proposal already accepted or rejected")
	}
	return errors.WithMessage(p.responder.Reject(ctx, reason), "rejecting channel proposal")
}
