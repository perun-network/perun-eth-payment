// Copyright (c) 2020 Chair of Applied Cryptography, Technische Universität
// Darmstadt, Germany. All rights reserved. This file is part of
// perun-eth-mobile. Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"sync"

	host "github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"
	swarm "github.com/libp2p/go-libp2p-swarm"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"

	"perun.network/go-perun/wallet"
	"perun.network/go-perun/wire"

	wirenet "perun.network/go-perun/wire/net"
)

// DialerP2P is a TCP dialer and implements:
// ref https://pkg.go.dev/perun.network/go-perun@v0.6.0/wire/net#Dialer
type DialerP2P struct {
	myHost               host.Host // LibP2P host
	serverAddr, serverID string    // relay server

	mutex sync.RWMutex
	peers map[wallet.AddrKey]string
}

// NewTCPDialerP2P sets up the dialerp2p.
func NewTCPDialerP2P(host host.Host, serverAddr, serverID string) *DialerP2P {
	return &DialerP2P{
		myHost:     host,
		serverAddr: serverAddr,
		serverID:   serverID,
		peers:      make(map[wallet.AddrKey]string),
	}
}

// getPeerId returns the peerId to a given Ethereum address.
func (d *DialerP2P) getPeerId(key wallet.AddrKey) (string, error) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	peerID, ok := d.peers[key]
	if !ok {
		return "", errors.New("PeerId for given address not found")
	}
	return peerID, nil
}

// Dial implements Dialer.Dial().
func (d *DialerP2P) Dial(_ context.Context, addr wire.Address) (wirenet.Conn, error) {
	peerID, err := d.getPeerId(wallet.Key(addr))
	if err != nil {
		return nil, err
	}

	anotherClientID, err := peer.Decode(peerID)
	if err != nil {
		return nil, errors.Wrap(err, "peer id is not valid")
	}

	fullAddr := d.serverAddr + "/p2p/" + d.serverID + "/p2p-circuit/p2p/" + anotherClientID.Pretty()
	anotherClientMA, err := ma.NewMultiaddr(fullAddr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse multiaddress of another peer")
	}

	// Redialing hacked.
	d.myHost.Network().(*swarm.Swarm).Backoff().Clear(anotherClientID)
	anotherClientInfo := peer.AddrInfo{
		ID:    anotherClientID,
		Addrs: []ma.Multiaddr{anotherClientMA},
	}
	if err := d.myHost.Connect(context.Background(), anotherClientInfo); err != nil {
		return nil, errors.Wrap(err, "failed to dial peer: failed to connecting to peer")
	}

	// Connecting.
	s, err := d.myHost.NewStream(context.Background(), anotherClientInfo.ID, "/client")
	if err != nil {
		return nil, errors.Wrap(err, "failed to dial peer: failed to creating a new stream")
	}

	return wirenet.NewIoConn(s), nil
}

// Close closes the libp2p host.
func (d *DialerP2P) Close() error {
	err := d.myHost.Close()
	return err
}

// Register registers a libp2p peer id for a peer address.
func (d *DialerP2P) Register(addr wire.Address, peerID string) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.peers[wallet.Key(addr)] = peerID
}
