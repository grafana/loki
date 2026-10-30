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

// Package session hosts the vRPC-over-session data-plane API that the
// public bigtable package composes with the classic gRPC data-plane
// via TableShim.
//
// The split exists because a proto-native surface — one that takes and
// returns *SessionReadRowRequest / *SessionReadRowResponse instead of
// bigtable.Row — is a materially different concern from the classic
// TableAPI. The two live behind TableShim, which routes between them
// via a Diverter and owns proto ↔ bigtable.Row conversion. Nothing in
// this package imports the top-level bigtable package.
package session

import (
	"context"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	btransport "cloud.google.com/go/bigtable/internal/transport"
	"go.opentelemetry.io/otel/metric"
)

// TableAPI is the per-resource, proto-native API exposed to
// TableShim. The concrete implementation routes ReadRow over a READ
// session pool and MutateRow over a separate WRITE session pool —
// callers do not see the distinction. Pools open lazily on first
// call (see lazyPool) so read-only resources never pay for a write
// pool, and construction of a TableAPI never dials.
type TableAPI interface {
	ReadRow(ctx context.Context, req *btpb.SessionReadRowRequest) (*btpb.SessionReadRowResponse, error)
	MutateRow(ctx context.Context, req *btpb.SessionMutateRowRequest) (*btpb.SessionMutateRowResponse, error)

	// Close releases this resource's underlying read + write session
	// pools from the sessionClient's per-resource keyed map. Idempotent.
	// Independent from Client.Close — closing an individual resource
	// does not close the shared channel pool.
	//
	// Per-handle pool teardown is safe because callers reach this method
	// through bigtable.Client's session.TableCache, which guarantees
	// at-most-one TableAPI per resource per Client. A future caller that
	// bypasses that cache must add a refcount before calling Close.
	Close() error
}

// DebugAccess exposes internal snapshots for the sessionz / configz /
// channelz debug pages. Kept separate from Client to keep the
// primary interface focused on data-plane concerns; consumers type-
// assert (Client).(DebugAccess) when they need it.
type DebugAccess interface {
	// PoolSnapshots returns one PoolSnapshot per owned pool, ordered
	// by pool key. Feeds sessionz.
	PoolSnapshots() []btransport.PoolSnapshot
	// LoadBalancingSnapshots returns per-pool picker + pick-history
	// snapshots. Feeds loadz.
	LoadBalancingSnapshots() []btransport.LoadBalancingSnapshot
	// ChannelPool returns the *BigtableChannelPool the Client
	// was constructed with, or nil.
	ChannelPool() *btransport.BigtableChannelPool
	// ConfigManager returns the internal ClientConfigurationManager
	// for configz. Nil when no stub was provided at construction.
	ConfigManager() *btransport.ClientConfigurationManager
}

// Client owns the underlying gRPC channel pool + stub and vends
// per-resource TableAPI instances. Does NOT cache — callers
// (bigtable.Client) are responsible for caching per-resource entries
// so repeat Opens reuse the same underlying pools.
type Client interface {
	// OpenTable returns a TableAPI for a standard table,
	// identified by the leaf table name (e.g. "my-table"). Full
	// resource composition happens inside the implementation.
	OpenTable(tableID string) TableAPI

	// OpenAuthorizedView returns a TableAPI for a specific
	// authorized view under `table`.
	OpenAuthorizedView(table, view string) TableAPI

	// OpenMaterializedView returns a read-only TableAPI for a
	// materialized view. MutateRow on the returned handle errors.
	OpenMaterializedView(view string) TableAPI

	// MeterProvider exposes the OpenTelemetry meter provider the
	// Client was constructed with — same instance the
	// bigtable client uses for its own metrics, so callers can
	// register additional instruments against the same provider.
	MeterProvider() metric.MeterProvider

	// SessionDebug / ChannelDebug / ConfigDebug expose the debug-page
	// data surfaces. Together they satisfy the same shape
	// debugview.DebugProviders needs, so a Client (or a public
	// wrapper composed of one) can be handed to debugview.Handler
	// without an adapter. Diverter() on the returned SessionDebugProvider
	// is empty for standalone session.Client — the classic/session
	// split is a mixed-mode concept that only makes sense on a
	// bigtable.Client that also owns a classic pool.
	SessionDebug() btransport.SessionDebugProvider
	ChannelDebug() btransport.ChannelDebugProvider
	ConfigDebug() btransport.ConfigDebugProvider

	// AddSessionLoadListener registers a listener invoked every time
	// the server-driven ClientConfigurationManager reports a new
	// session-load ratio (0.0 = classic-only, 1.0 = session-only).
	// Returns an unregister thunk. Used by mixed-mode bigtable.Client
	// to feed its Diverter; standalone session.Client callers can
	// ignore this method.
	AddSessionLoadListener(func(load float64)) func()

	// Close closes the underlying channel pool. TableAPI
	// instances previously vended become unusable.
	Close() error
}
