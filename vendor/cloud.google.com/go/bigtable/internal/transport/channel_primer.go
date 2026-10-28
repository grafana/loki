// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import (
	"context"

	btopt "cloud.google.com/go/bigtable/internal/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ChannelPrimer warms a freshly-dialed Bigtable channel before it is put
// into rotation. The pool's connection factory consults the registered
// primer after every successful dial; a nil ChannelPrimer means the pool
// skips priming entirely and hands the raw connection straight to the
// pool.
//
// Implementations differ in HOW the channel is warmed:
//   - pingAndWarmChannelPrimer issues a PingAndWarm against the configured
//     instance / app profile with the supplied feature-flag metadata
//     (today's only behavior, used by the classic channel pool factory).
//   - NoOpChannelPrimer skips priming entirely. Prefer this over nil at
//     construction sites where "no priming" is a deliberate architectural
//     choice (e.g. session channel pools that warm via OpenSession on
//     each stream) rather than a placeholder waiting for a real primer.
type ChannelPrimer interface {
	// Prime warms conn so the next request served by it does not pay the
	// first-RPC connection-setup cost. The factory wraps Prime in a retry
	// loop, so transient errors should propagate as-is.
	Prime(ctx context.Context, conn *BigtableConn) error
}

// NoOpChannelPrimer explicitly disables per-connection priming. Session
// channel pools warm their channels via the OpenSession handshake on
// each newly-opened stream, so a PingAndWarm at dial time is redundant.
// Passing NoOpChannelPrimer{} makes the "no priming" choice explicit at
// the construction site instead of relying on nil-as-sentinel.
type NoOpChannelPrimer struct{}

// Prime is a no-op — the channel is returned unprimed.
func (NoOpChannelPrimer) Prime(_ context.Context, _ *BigtableConn) error {
	return nil
}

// pingAndWarmChannelPrimer primes a channel by issuing a PingAndWarm RPC
// against the configured instance + app profile, carrying the supplied
// feature-flag metadata. Stateless aside from the configured identifiers,
// so a single primer instance is shared across all dials in a pool.
type pingAndWarmChannelPrimer struct {
	instanceName   string
	appProfile     string
	featureFlagsMD metadata.MD
}

// newPingAndWarmChannelPrimer constructs the today-default channel primer.
func newPingAndWarmChannelPrimer(instanceName, appProfile string, featureFlagsMD metadata.MD) *pingAndWarmChannelPrimer {
	return &pingAndWarmChannelPrimer{
		instanceName:   instanceName,
		appProfile:     appProfile,
		featureFlagsMD: featureFlagsMD,
	}
}

// Prime delegates to BigtableConn.Prime, which sends PingAndWarm and
// records the ALTS / IP-protocol observations on conn as a side effect.
//
// A NotFound response is treated as a successful prime: the server
// returns NotFound for a valid but empty instance ("No tables found for
// instance ..."), and blocking client creation in that case would
// prevent admin flows (create the first table, migrations). Typo'd
// instance names also produce NotFound but surface on the first real
// RPC, so nothing is silently masked here.
func (p *pingAndWarmChannelPrimer) Prime(ctx context.Context, conn *BigtableConn) error {
	err := conn.Prime(ctx, p.instanceName, p.appProfile, p.featureFlagsMD)
	if err != nil && status.Code(err) == codes.NotFound {
		btopt.Debugf(nil, "bigtable_connpool: PingAndWarm returned NotFound (instance likely empty); treating as primed: %v", err)
		return nil
	}
	return err
}
