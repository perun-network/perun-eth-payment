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
	"perun.network/go-perun/wire/net"
)

// ClientOption is an option that can be passed to `NewClient`
// to change the default behaviour of a client.
type ClientOption func(*Client)

// WithDialer can be used to pass a custom dialer into the Client.
// The configuration option `DialTimeout` will be unused in this case.
func WithDialer(dialer Dialer) ClientOption {
	return func(client *Client) {
		client.dialer = dialer
	}
}

// WithListener can be used to pass a custom listener into the Client.
// The configuration option `Host` will be unused in this case.
func WithListener(listener net.Listener) ClientOption {
	return func(client *Client) {
		client.listener = listener
	}
}
