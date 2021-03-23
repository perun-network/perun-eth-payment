// Copyright (c) 2020 Chair of Applied Cryptography, Technische Universität
// Darmstadt, Germany. All rights reserved. This file is part of
// perun-eth-mobile. Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package main

import (
	"io"
	"time"

	host "github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"

	wirenet "perun.network/go-perun/wire/net"
)

// ListenerP2P is a TCP listener and implements:
// ref https://pkg.go.dev/perun.network/go-perun@v0.6.0/wire/net#Listener
type ListenerP2P struct {
	myHost host.Host
	myRwc  io.ReadWriteCloser // the newest incoming connection stream
}

// NewTCPListenerP2P sets the protocol handler of the libp2p host.
func NewTCPListenerP2P(host host.Host) (*ListenerP2P, error) {
	myListener := ListenerP2P{myHost: host, myRwc: nil}
	host.SetStreamHandler("/client", func(s network.Stream) {
		myListener.myRwc = s
	})
	return &myListener, nil
}

// Accept waits for incoming connection and returns a new I/O connection.
func (l *ListenerP2P) Accept() (wirenet.Conn, error) {
	var tmp io.ReadWriteCloser
	for {
		time.Sleep(100 * time.Millisecond)
		if l.myRwc != nil {
			tmp = l.myRwc
			l.myRwc = nil
			break
		}
	}
	return wirenet.NewIoConn(tmp), nil
}

// Close closes the libp2p host.
func (l *ListenerP2P) Close() error {
	err := l.myHost.Close()
	return err
}
