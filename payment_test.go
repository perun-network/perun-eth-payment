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
	"crypto/rand"
	"math/big"
	"testing"

	payment "github.com/perun-network/perun-eth-payment"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pkgtest "perun.network/go-perun/pkg/test"
)

const floatEps = 0.00000001

func TestEthAmount(t *testing.T) {
	rng := pkgtest.Prng(t)

	payment.SetLogLevel(logrus.WarnLevel, &logrus.TextFormatter{})
	t.Run("MakeAmountFromETH", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			f := rng.Float64() * 1000
			v := payment.MakeAmountFromETH(f)
			assert.InDelta(t, f, v.ETH(), floatEps)
			f2, _ := new(big.Float).SetInt(v.WEI()).Float64()
			assert.InDelta(t, f*1e18, f2, floatEps)
		}
	})

	t.Run("MakeAmountFromWEI", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			// Max is 2^67 ~ 1000 ETH
			f, err := rand.Int(rng, new(big.Int).Lsh(big.NewInt(1), 67))
			require.NoError(t, err)

			v := payment.MakeAmountFromWEI(f)
			assert.Equal(t, f, v.WEI())
			f2, _ := new(big.Float).SetInt(f).Float64()
			assert.InDelta(t, f2/1e18, v.ETH(), floatEps)
		}

		assert.Panics(t, func() {
			payment.MakeAmountFromWEI(nil)
		})
	})
}
