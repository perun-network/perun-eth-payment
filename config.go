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
	"math"
	"time"
)

// Config contains all configuration options that are needed to create a Client.
// Should be created with `MakeConfig`.
type Config struct {
	Host, ChainURL     string
	ChallengeDuration  time.Duration
	DialTimeout        time.Duration
	ProposalBufferSize uint
	UpdateBufferSize   uint
}

// MakeConfig returns a new Config. All options that are not passed as
// arguments are set to default values. You can still modify them manually.
// `host` is the ip:port that the client should listen on for connections.
// `chainURL` is the URL of your Ethereum node.
// In the local ganache-cli case this would be: ws://0.0.0.0:8545
// `challengeDuration` is the time in seconds that an on-chain challenge
// will last. This should be at least 3 times the average block time.
func MakeConfig(host, chainURL string, challengeDuration time.Duration) Config {
	return Config{
		Host:               host,
		ChainURL:           chainURL,
		ChallengeDuration:  challengeDuration,
		DialTimeout:        10 * time.Second,
		ProposalBufferSize: 10,
		UpdateBufferSize:   10,
	}
}

// challengeDurationSec returns the challenge duration in seconds rounded up.
func (c Config) challengeDurationSec() uint64 {
	return uint64(math.Ceil(c.ChallengeDuration.Seconds()))
}
