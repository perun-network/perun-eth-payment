package main

import (
	"context"
	stdnet "net"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p"
	p2pcrypto "github.com/libp2p/go-libp2p-core/crypto"
	host "github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/perun-network/perun-eth-payment/payment"
	"github.com/pkg/errors"
	"perun.network/go-perun/log"
	"perun.network/go-perun/wire"
)

func SetupClient(cfg Config) (*payment.Client, *payment.Wallet, error) {
	wallet, err := payment.NewWalletSK(cfg.ChainID, cfg.SK)
	if err != nil {
		return nil, nil, errors.WithMessage(err, "creating wallet")
	}
	log.Infof("On-Chain Address: %s", wallet.Address.String())

	host, serverAddr, err := CreateClientHost(cfg)
	if err != nil {
		return nil, nil, errors.WithMessagef(err, "creating libp2p client host")
	}

	listener, err := NewTCPListenerP2P(host) // Creates a libp2p listener.
	if err != nil {
		return nil, nil, errors.WithMessagef(err, "listening on %s", host)
	}

	dialer := NewTCPDialerP2P(host, serverAddr, cfg.ServerID) // Creates a libp2p dialer.
	register := func(addr wire.Address, alias string) { dialer.Register(addr, alias) }

	clientCfg := payment.MakeConfig("not-needed", cfg.ETHNodeURL, 60*time.Second)
	client, err := payment.NewClient(wallet, clientCfg, dialer, register, listener)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.DeployTimeout)
	defer cancel()
	err = client.Init(ctx, cfg.Adj, cfg.Ah)
	return client, wallet, err
}

// CreateClientHost creates a libp2p host connecting to a relay-server.
func CreateClientHost(cfg Config) (host.Host, string, error) {
	// Parse Relay Peer ID
	id, err := peer.Decode(cfg.ServerID)
	if err != nil {
		return nil, "", errors.WithMessage(err, "decoding peer id of relay server")
	}

	// Use IP address of 'relay.perun.network'.
	ips, err := stdnet.LookupIP("relay.perun.network")
	if err != nil {
		return nil, "", errors.WithMessage(err, "looking up IP addresses of relay.perun.network")
	}
	serverAddr := "/ip4/" + ips[0].String() + "/tcp/5574"

	// Parse relay's multiaddress.
	tmp, err := ma.NewMultiaddr(serverAddr)
	if err != nil {
		return nil, "", errors.WithMessage(err, "parsing relay multiadress")
	}
	addrs := []ma.Multiaddr{tmp}

	// Init relay's AddrInfo.
	relayInfo := peer.AddrInfo{
		ID:    id,
		Addrs: addrs,
	}

	// Create private key from given ESCDA secret key.
	data, err := crypto.HexToECDSA(cfg.SK)
	if err != nil {
		return nil, "", errors.WithMessage(err, "parsing secp256k1 private key")
	}
	prvKey, err := p2pcrypto.UnmarshalSecp256k1PrivateKey(data.X.Bytes())
	if err != nil {
		return nil, "", errors.WithMessage(err, "unmarshaling secp256k1 private key")
	}

	// Construct a new libp2p client for our relay-server.
	// Identity(prvKey)	- Use a private key to generate the ID of the host.
	client, err := libp2p.New(
		context.Background(),
		libp2p.EnableRelay(),
		libp2p.Identity(prvKey),
	)
	log.Infof("PeerID: %v", client.ID())

	if err != nil {
		return nil, "", errors.WithMessage(err, "constructing a new libp2p node")
	}

	// Connect to relay server.
	if err := client.Connect(context.Background(), relayInfo); err != nil {
		return nil, "", errors.WithMessage(err, "connecting to the relay server")
	}

	return client, serverAddr, nil
}

func valid(cfg Config, prop *payment.ChannelProposal) error {
	if prop.Balance.My.ETH() > cfg.MaxFunding.ETH() {
		return errors.New("initial funds too large")
	}
	return nil
}
