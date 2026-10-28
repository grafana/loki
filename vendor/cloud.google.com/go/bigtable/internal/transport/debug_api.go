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

// The debug provider surface lives here (not in the top-level bigtable
// package) so both bigtable.Client and internal/session.Client can
// implement it without an import cycle. bigtable/session_debug.go
// re-exports the same names as type aliases so external consumers see
// unchanged bigtable.SessionDebugProvider / ChannelDebugProvider /
// ConfigDebugProvider identifiers.

// SessionDebugProvider exposes a snapshot of every session pool a client
// currently owns. The debugview package consumes this to render sessionz /
// afez / loadz; callers can also use it programmatically.
//
// Snapshot is safe to call concurrently with any other client operation;
// it adds no overhead to the RPC hot path — underlying counters are
// incremented atomically and the snapshot path reads them lock-free.
type SessionDebugProvider interface {
	// Snapshot returns a snapshot of every session pool, ordered by
	// pool key.
	Snapshot() []PoolSnapshot
	// Diverter returns the client-wide session/classic split state. For
	// standalone SessionClient (no classic path) callers return an
	// empty DiverterSnapshot.
	Diverter() DiverterSnapshot
	// LoadBalancingSnapshots returns one per-pool picker + pick-history
	// snapshot for the loadz debug page.
	LoadBalancingSnapshots() []LoadBalancingSnapshot
}

// ChannelDebugProvider exposes a snapshot of every gRPC channel pool the
// client currently owns — the classic data-plane pool and, when session
// pooling is enabled, the dedicated session pool.
type ChannelDebugProvider interface {
	// Snapshot returns one ChannelPoolDebug per BigtableChannelPool the
	// client holds, labeled by Role ("classic" / "session").
	Snapshot() []ChannelPoolDebug
}

// ChannelPoolDebug names a single channel pool and carries its snapshot.
type ChannelPoolDebug struct {
	// Role labels the pool — "classic" for the data-plane pool,
	// "session" for the dedicated session pool created when session
	// pooling is enabled.
	Role     string
	Snapshot ChannelPoolSnapshot
	// SessionsByChannel maps a connEntry index to the sessions riding
	// on it. Populated only for the "session" role.
	SessionsByChannel map[int][]SessionRef
}

// SessionRef identifies one session for the channelz → sessionz reverse
// link. PoolName matches the sessionz /pool/{name} URL segment; LogName
// is the per-session row anchor (id="session-{LogName}") in sessionz.
type SessionRef struct {
	PoolName string
	LogName  string
}

// ConfigDebugProvider exposes a snapshot of the most recent
// GetClientConfiguration poll outcome. The debugview package consumes
// this to render configz.
type ConfigDebugProvider interface {
	// Snapshot returns the most recent GetClientConfiguration response
	// (or the most recent error if no poll has succeeded yet).
	Snapshot() ConfigSnapshot
}
