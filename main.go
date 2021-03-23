package main

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/perun-network/perun-eth-payment/app"
	"github.com/perun-network/perun-eth-payment/payment"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"perun.network/go-perun/log"
)

func main() {
	payment.SetLogLevel(logrus.InfoLevel, &logrus.TextFormatter{})
	payment.SetApp(&app.PaymentApp{})

	cfg := Config{
		ETHNodeURL: "wss://goerli.infura.io/ws/v3/2db3b60c0f1a441a86f4dc093fb58d33",
		ChainID:    big.NewInt(5),

		SK:       "",
		ServerID: "QmVCPfUMr98PaaM8qbAQBgJ9jqc7XHpGp7AsyragdFDmgm",

		Adj: common.HexToAddress("0x5Bd2EaFb58939D806516dbf64Df99cCdFedE05F9"),
		Ah:  common.HexToAddress("0xa6074B1895548B8114aBC3D3F96853f7C4657495"),

		FundTimeout:   600 * time.Second,
		SendTimeout:   600 * time.Second,
		CloseTimeout:  600 * time.Second,
		DeployTimeout: 600 * time.Second,

		MaxFunding: payment.MakeAmountFromETH(0.1),
	}
	client, wallet, err := SetupClient(cfg)
	if err != nil {
		panic(err)
	}
	if err := Run(cfg, client, wallet); err != nil {
		panic(err)
	}
	log.Info("Finished")
}

func Run(cfg Config, c *payment.Client, w *payment.Wallet) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bals, err := c.OnChainBalance(ctx, w.Address)
	if err != nil {
		return err
	}
	bal := bals[0]
	// bot needs at least 1 ETH
	minBal := new(big.Int).Mul(big.NewInt(params.Ether), big.NewInt(1))
	if bal.Cmp(minBal) < 0 {
		return errors.New("too few ether on bot account")
	}

	log.Info("Listening on channel proposals…")
	// Accept channels.
	for prop := range c.Proposals() {
		log.Infof("Received proposal from: %s", prop.From.Addr())
		fundCtx, cancel := context.WithTimeout(ctx, cfg.FundTimeout)
		defer cancel()

		if err := valid(cfg, prop); err != nil {
			log.WithError(err).Warn("Rejecting invalid proposal")
			if err := prop.Reject(fundCtx, err.Error()); err != nil {
				log.WithError(err).Error("Failed to reject invalid proposal")
			}
			continue
		}
		log.Info("Accepting proposal")

		ch, err := prop.Accept(fundCtx)
		if err != nil {
			log.WithError(err).Errorf("Could accept proposal from: %s", prop.From.Addr())
			continue
		}
		go HandleChannel(ctx, cfg, ch, prop.From.Addr())
	}
	return nil
}

func HandleChannel(ctx context.Context, cfg Config, ch *payment.Channel, alias string) {
	log := log.WithField("alias", alias)
	// 1.
	log.Info("HandleChannel - echoing")
loop:
	for i := 0; i < 3; i++ {
		received := <-ch.Received()
		if received == nil {
			log.Info("Received nil state, closing")
			break loop
		}
		log.Infof("Received: %f ETH - sleeping", received.ETH())
		time.Sleep(1 * time.Second)
		log.Info("Echoing")
		sendCtx, cancel := context.WithTimeout(ctx, cfg.SendTimeout)
		defer cancel()

		half := new(big.Int).Div(received.WEI(), big.NewInt(2))
		amount := payment.MakeAmountFromWEI(half)

		log.Infof("Sending: %v", amount.ETH())
		if err := ch.Send(sendCtx, amount); err != nil {
			log.WithError(err).Error("Could not echo payment")
		}
		time.Sleep(1 * time.Second)
		log.Infof("Sending: %v", amount.ETH())
		if err := ch.Send(sendCtx, amount); err != nil {
			log.WithError(err).Error("Could not echo payment")
		}
	}
	// 2.
closing:
	for {
		log.Info("HandleChannel - closing")
		closeCtx, cancel := context.WithTimeout(ctx, cfg.CloseTimeout)
		defer cancel()
		if err := ch.Close(closeCtx); err != nil {
			log.WithError(err).Error("Could not close channel")
			continue closing
		}
		log.Info("HandleChannel finished")
		break closing
	}
}
