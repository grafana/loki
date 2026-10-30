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

package session

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	metrics "cloud.google.com/go/bigtable/internal/metrics"
	btopt "cloud.google.com/go/bigtable/internal/option"
	btransport "cloud.google.com/go/bigtable/internal/transport"
	otelmetric "go.opentelemetry.io/otel/metric"
	"google.golang.org/api/option"
	gtransport "google.golang.org/api/transport/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// defaultSessionChannelPoolSize seeds the session channel pool with a
// small footprint. The server-driven ClientConfigurationManager
// reshapes this at runtime via SessionClientConfiguration polls, so
// the initial size only has to carry traffic long enough for the
// first poll to return. Session pools intentionally IGNORE
// option.WithGRPCConnectionPool(N) — pool shape is server-driven
// end-to-end.
const defaultSessionChannelPoolSize = 10

// Standard gRPC routing headers — aliased to the shared exports in
// internal/transport so the session package and the top-level bigtable
// package (which keeps its own copies for back-compat) don't drift.
const (
	resourcePrefixHeader = btransport.ResourcePrefixHeader
	requestParamsHeader  = btransport.RequestParamsHeader
)

// sessionProtocolVersion is the wire-protocol version stamped on every
// OpenSessionRequest envelope. Bump when we introduce a
// non-backwards-compatible change to the request/response shape or the
// vRPC descriptor set.
const sessionProtocolVersion = 1

// ChannelPool is the narrow surface sessionClient needs from the
// managed channel pool it owns. Satisfied by
// *btransport.BigtableChannelPool. Interfaced so tests can swap in a
// fake pool without wiring a real gRPC transport.
type ChannelPool interface {
	Close() error
}

// Config bundles the settings sessionClient needs at construction
// time. Kept as a struct rather than a long positional constructor.
type Config struct {
	// Project / Instance / AppProfile identify the target resource
	// and get baked into resource-name composition + request-params.
	Project    string
	Instance   string
	AppProfile string

	// FeatureFlagsMD is merged into per-pool routing metadata.
	FeatureFlagsMD metadata.MD

	// ConfigMD is the metadata attached to the ClientConfigurationManager's
	// GetClientConfiguration polls — instance-scoped headers.
	ConfigMD metadata.MD

	// MetricsEnabled mirrors the SessionManager boolean of the same
	// name; propagates into FeatureFlags on every OpenSessionRequest.
	MetricsEnabled bool

	// FeatureFlagsProto is the pre-built FeatureFlags proto stamped
	// onto every OpenSessionRequest.Flags. Required — callers MUST
	// populate this with the same proto they marshaled into
	// FeatureFlagsMD so header and envelope are byte-identical (the
	// server rejects OpenSession with INVALID_ARGUMENT on mismatch).
	FeatureFlagsProto *btpb.FeatureFlags

	// SessionLoadListener is invoked whenever the server-driven
	// ClientConfigurationManager reports a new session-load ratio. The
	// bigtable Client wires this to Diverter.SetSessionLoad so the
	// classic/session traffic split follows the server's directive.
	SessionLoadListener func(load float64)

	// BackgroundCtx is the parent context for the Client's own
	// process-lifetime loops (today: ClientConfigurationManager polling).
	// Cancelled by Client teardown.
	//
	// Deliberately NOT handed to session pools. A pool's lifetime is a
	// strict subset of this Client's: Close() closes every pool, and
	// session.TableCache also evicts + Closes individual pools while the
	// Client keeps running. So this ctx is always too coarse to scope
	// pool-owned work — it can only ever fire after the pool is already
	// gone (client shutdown), or never at all (TTL eviction). Either way
	// the pool's loops outlive its Close, and since each loop closure
	// captures the pool, the pool itself stays reachable. Pools scope
	// their own goroutines; see SessionPoolImpl.Start / maintCtx.
	BackgroundCtx context.Context

	// EnableDebug controls whether the pools this Client mints will
	// collect per-pool snapshot state (sessionz / afez / flightz /
	// loadz). Default false. When false, every allocating debug
	// recorder in the pool is skipped for zero hot-path overhead —
	// no per-session events ring, no latency-sample buffers, no
	// per-pick candidate slices, no pool-wide histogram inserts, no
	// slow-vRPC log entries. SessionDebug() also returns nil so the
	// debugview handler renders a "not enabled" panel.
	//
	// Callers that plan to serve /debug/ from bigtable/debugview or
	// scrape session snapshots programmatically should set this true;
	// production workloads that only care about the OTel metrics
	// (attempt_latencies / operation_latencies / etc.) can leave it
	// off. The debug surface is otherwise unchanged; flipping the
	// flag on or off requires rebuilding the client.
	EnableDebug bool
}

// managedSessionPool bundles a pool with its config-listener unregister
// thunk so the listener can be detached before pool teardown. id and key
// are carried only for the observational index (sessionz snapshots,
// stable ordering, config-listener fanout) — the pool's lifetime is
// owned by the poolCloser handed back to the sessionTable that created
// it, NOT by its presence in this map.
type managedSessionPool struct {
	id         uint64
	key        poolKey
	pool       *btransport.SessionPoolImpl
	unregister func()
}

// poolCloser owns the teardown of the single pool instance created by
// one buildLazyOpener invocation. The opener calls set() with an
// identity-scoped release thunk once the pool is constructed; Close()
// runs it exactly once. Close() before set() (opener never ran — the
// resource was never touched) or after a prior Close() is a no-op.
//
// This gives each sessionTable single ownership of its pool: a table
// closes exactly the pool its OWN opener created, by captured identity,
// never a sibling's pool that happens to share a poolKey. That prevents
// closing a pool a live sibling sessionTable is still memoizing.
type poolCloser struct {
	mu      sync.Mutex
	release func() error
	done    bool
}

// set records the identity-scoped release thunk for the pool the opener
// just created. If Close() already ran (the sessionTable was torn down
// while the opener was mid-flight), set tears the just-created pool down
// immediately so it doesn't leak, and reports that the owner is closed.
func (c *poolCloser) set(release func() error) (closed bool, err error) {
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return true, release()
	}
	c.release = release
	c.mu.Unlock()
	return false, nil
}

// Close runs the captured release thunk exactly once. No-op when the
// opener never ran (release nil) or Close already fired.
func (c *poolCloser) Close() error {
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return nil
	}
	c.done = true
	release := c.release
	c.release = nil
	c.mu.Unlock()
	if release == nil {
		return nil
	}
	return release()
}

// permission is the read/write axis of a pool's identity. Kept as a
// typed enum (not a string suffix on the map key) so createSessionPool
// doesn't have to reverse-parse the key to label the pool.
type permission int

const (
	permissionRead permission = iota
	permissionWrite
)

// display returns the label used in pool display names (e.g. "READ").
func (p permission) display() string {
	switch p {
	case permissionRead:
		return "READ"
	case permissionWrite:
		return "WRITE"
	}
	return ""
}

// poolKey names a pool for display + ordering only. Resource is the
// caller-supplied short name ("table:foo", "av:t:v", "mv:v"); permission
// separates the read pool from the write pool for the same resource.
//
// poolKey is deliberately NOT the ownership key: pools are owned by the
// poolCloser handed to the creating sessionTable and indexed in
// sessionClient.sessionPools by a monotonic uint64 id. Two live
// sessionTables for the same resource therefore each own a private pool
// that happens to share a poolKey — which is exactly the property that
// fixes the shared-pool close-forever bug. The key survives only to label
// the OTel `session_name` metric (displayName) and to order snapshots
// (less).
type poolKey struct {
	resourceName string
	perm         permission
}

// less orders poolKeys for stable snapshot rendering (resource first,
// then permission).
func (k poolKey) less(other poolKey) bool {
	if k.resourceName != other.resourceName {
		return k.resourceName < other.resourceName
	}
	return k.perm < other.perm
}

// displayName renders the human-readable pool identity that is stamped
// as the OTel `session_name` metric label (via WithSessionPoolName)
// and rendered in sessionz. Format: "<resource-id>-<PERM>".
//
// Resource-id is the caller-supplied short name with the internal
// type-prefix stripped:
//
//	table:<id>    → "<id>-<PERM>"            e.g. "my-table-READ"
//	av:<t>:<v>    → "<t>/<v>-<PERM>"         e.g. "my-table/my-view-READ"
//	mv:<v>        → "<v>-<PERM>"             e.g. "my-view-READ"
//
// For authorized views the table qualifier is preserved as "<table>/<view>"
// (converted from the "av:" pool-key encoding "av:<table>:<view>") so
// two AVs with the same view id on different tables produce distinct
// `session_name` timeseries and distinct sessionz rows — otherwise they
// would silently aggregate on the label.
//
// The numeric pool id is intentionally left out of the display name so
// `session_name` label cardinality stays bounded by (resource ×
// permission) even though several short-lived pools may share a
// (resource, permission) across successive Open* calls. Those pools
// aggregate onto one `session_name` timeseries by design; the numeric id
// still lives on SessionPoolImpl (as poolID) for the sessionz ↔ channelz
// reverse link and per-session log names.
func (k poolKey) displayName() string {
	r := k.resourceName
	switch {
	case strings.HasPrefix(r, "table:"):
		r = strings.TrimPrefix(r, "table:")
	case strings.HasPrefix(r, "mv:"):
		r = strings.TrimPrefix(r, "mv:")
	case strings.HasPrefix(r, "av:"):
		// "av:<table>:<view>" → "<table>/<view>" so the label
		// disambiguates AVs with the same view id on different tables.
		r = strings.Replace(strings.TrimPrefix(r, "av:"), ":", "/", 1)
	}
	return r + "-" + k.perm.display()
}

// sessionClient is the internal implementation of the Client
// interface. Owns the channel pool + gRPC stub + configuration
// manager, and vends per-resource TableAPI instances.
//
// The channel pool + stub + metrics factory are OWNED — Close() closes
// all three. backgroundCancel unwinds every per-pool goroutine parented
// on the internally-created background ctx.
type sessionClient struct {
	cfg            Config
	channelPool    ChannelPool
	stub           btpb.BigtableClient
	metricsFactory *metrics.Factory
	// configManager runs the server-driven GetClientConfiguration poll
	// loop for the lifetime of this Client. Production runtime state
	// (not debug/test) — its output drives Diverter.SetSessionLoad,
	// per-pool UpdateConfig reshapes (bounds, picker, load-balancing
	// strategy), and future eager-scale directives. Also surfaced on
	// /debug/configz for operator visibility.
	configManager    *btransport.ClientConfigurationManager
	backgroundCancel context.CancelFunc // release when Close() runs
	// managed carries the DynamicScaleMonitor + ConnectionRecycler that
	// the shared btransport.CreateAndStartManagedChannelPool wires up.
	// Close() unwinds both by calling managed.Close(). Zero-value for
	// the test factory (newSessionClientFromParts with a fake pool):
	// managed.Pool == nil, so Close falls back to sc.channelPool.Close().
	managedChannelPool btransport.ManagedChannelPool

	// enableDebug mirrors Config.EnableDebug. When false, pools this
	// client mints short-circuit every allocating debug recorder and
	// SessionDebug() returns nil so /debug/ renders a "not enabled"
	// panel. Immutable after construction — no atomic needed.
	enableDebug bool

	// featureFlagsProto is the FeatureFlags proto stamped on every
	// OpenSessionRequest.Flags. Built once at NewClient time so it
	// stays byte-identical with the bigtable-features header — the
	// server rejects OpenSession with INVALID_ARGUMENT when the two
	// disagree on session-mode flags.
	featureFlagsProto *btpb.FeatureFlags

	// sessionPools is an observational index of live pools keyed by a
	// monotonic id (nextPoolID). It backs sessionz/PoolSnapshots, the
	// config-listener fanout, and Client.Close's close-all — NOT
	// ownership: each pool's lifetime is owned by the poolCloser handed
	// to the sessionTable that created it (see poolCloser). Entries are
	// added by createSessionPool and removed by the id-scoped releaser it
	// returns.
	sessionPoolsMu sync.Mutex
	sessionPools   map[uint64]*managedSessionPool
	nextPoolID     atomic.Uint64
}

// NewClient constructs a standalone session.Client. It owns the
// underlying channel pool, gRPC stub, metrics factory, and background
// goroutines end-to-end — Close() unwinds all four.
//
// The metricsProvider argument mirrors bigtable.ClientConfig.MetricsProvider
// (nil = built-in metrics enabled, NoopMetricsProvider{} = disabled).
// opts are the standard google.api option.ClientOption values passed to
// gtransport.Dial — endpoint, credentials, gRPC connection pool size,
// etc.
//
// Pool sizing bootstraps from btransport.defaultPoolConfig() and is
// overridden at runtime by the server-driven SessionClientConfiguration
// polls, which reshape live pools via SessionPoolImpl.UpdateConfig.
//
// The load-balancing hook for a mixed-mode setup lives at
// AddSessionLoadListener — call it after construction if you're
// composing this Client with a bigtable.Client Diverter.
//
// clientName sets the client_name attribute on exported client-side metrics.
// An empty string keeps the default "go-bigtable/<version>" token; the
// accelerator daemon passes its --user-agent flag so CSM attributes the metrics
// to the calling client library rather than the daemon build.
func NewClient(
	ctx context.Context,
	project, instance, appProfile, clientName string,
	metricsProvider metrics.MetricsProvider,
	featureFlagsProto *btpb.FeatureFlags,
	opts ...option.ClientOption,
) (Client, error) {
	factory, err := metrics.NewFactory(ctx, project, instance, appProfile, clientName, metricsProvider)
	if err != nil {
		return nil, fmt.Errorf("session.NewClient: metrics.NewFactory: %w", err)
	}

	// featureFlagsProto comes pre-built from the classic client so
	// header and envelope both derive from the same proto reference.
	// Marshal once here for the bigtable-features header.
	directAccessMD := btransport.MarshalFeatureFlagsMD(featureFlagsProto)

	fullInstance := fmt.Sprintf("projects/%s/instances/%s", project, instance)

	// Instance-scoped headers for GetClientConfiguration — shared by the
	// DirectAccessChecker's compat probe and ClientConfigurationManager's
	// steady-state polls so both hit the same-shaped RPC.
	configMD := buildConfigMetadata(fullInstance, appProfile, directAccessMD)

	// Session pool bypasses CreateAndStartManagedChannelPool so we can
	// plug in the session-flavored primer + direct-access checker
	// directly instead of adding config knobs to the classic helper.
	// NoOpChannelPrimer skips PingAndWarm on every sub-channel —
	// session channels warm on-demand via OpenSession bidi streams.
	// NewSessionClientDirectAccessChecker probes with
	// GetClientConfiguration (the same RPC ClientConfigurationManager
	// polls) instead of PingAndWarm, matching the actual on-wire RPC
	// mix a session pool serves.
	poolSize := defaultSessionChannelPoolSize
	dial := func() (*btransport.BigtableConn, error) {
		grpcConn, dialErr := gtransport.Dial(ctx, opts...)
		if dialErr != nil {
			return nil, dialErr
		}
		return btransport.NewBigtableConn(grpcConn), nil
	}
	// DirectAccess opt-set is centralized in btopt (shared with the
	// classic client and the DirectAccess probe) so the three call
	// sites cannot drift on which options are applied to a DirectPath
	// dial. See btopt.DirectAccessOptions for the rationale on each
	// individual option (in particular AllowNonDefaultServiceAccount,
	// without which the dial silently falls back to CFE+TLS under the
	// newer cloud.google.com/go/auth ADC path).
	daDialOpts := append(append([]option.ClientOption{}, opts...), btopt.DirectAccessOptions()...)
	daDial := func() (*btransport.BigtableConn, error) {
		grpcConn, dialErr := gtransport.Dial(ctx, daDialOpts...)
		if dialErr != nil {
			return nil, dialErr
		}
		return btransport.NewBigtableConn(grpcConn), nil
	}

	if err := btransport.ValidateDynamicConfig(btopt.DefaultDynamicChannelPoolConfig(), poolSize); err != nil {
		return nil, fmt.Errorf("session.NewClient: invalid DynamicChannelPoolConfig: %w", err)
	}
	pool, err := btransport.NewBigtableChannelPool(
		ctx,
		poolSize,
		btopt.BigtableLoadBalancingStrategy(),
		dial,
		time.Now(),
		btransport.WithMetricsReporterConfig(btopt.DefaultMetricsReporterConfig()),
		btransport.WithMeterProvider(factory.OtelMeterProvider),
		btransport.WithChannelPrimer(btransport.NoOpChannelPrimer{}),
		btransport.WithDirectAccessChecker(btransport.NewSessionClientDirectAccessChecker(
			daDial, fullInstance, appProfile, directAccessMD,
			factory.OtelMeterProvider, nil,
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("session.NewClient: NewBigtableChannelPool: %w", err)
	}
	dsm := btransport.NewDynamicScaleMonitor(btopt.DefaultDynamicChannelPoolConfig(), pool)
	dsm.Start(ctx)
	connRecycler := btransport.NewConnectionRecycler(btopt.DefaultConnectionRecycleConfig(), pool)
	connRecycler.Start(ctx)
	managed := btransport.NewManagedChannelPool(pool, dsm, connRecycler)
	stub := btpb.NewBigtableClient(pool)

	backgroundCtx, cancel := context.WithCancel(context.Background())

	sc := newSessionClientFromParts(pool, stub, factory, Config{
		Project:           project,
		Instance:          instance,
		AppProfile:        appProfile,
		FeatureFlagsMD:    directAccessMD,
		FeatureFlagsProto: featureFlagsProto,
		ConfigMD:          configMD,
		MetricsEnabled:    factory.Enabled,
		BackgroundCtx:     backgroundCtx,
		// EnableDebug intentionally left at zero (false): NewClient has
		// no external caller upstream today, so exposing a positional
		// bool on the constructor would ship a dead knob. When the
		// top-level bigtable.Client wiring lands, that PR can plumb
		// EnableClientDebug into this Config field directly.
	})
	sc.backgroundCancel = cancel
	sc.managedChannelPool = managed
	return sc, nil
}

// newSessionClientFromParts wires a sessionClient from already-built
// pool + stub + factory + Config. Extracted from the old public
// constructor so the new NewClient can share the assembly path.
// Unexported — no consumer outside this package should reach for it.
func newSessionClientFromParts(channelPool ChannelPool, stub btpb.BigtableClient, metricsFactory *metrics.Factory, cfg Config) *sessionClient {
	if cfg.MetricsEnabled && metricsFactory != nil {
		_ = btransport.InitializeSessionMetrics(metricsFactory.OtelMeterProvider)
	}
	sc := &sessionClient{
		cfg:               cfg,
		channelPool:       channelPool,
		stub:              stub,
		metricsFactory:    metricsFactory,
		enableDebug:       cfg.EnableDebug,
		featureFlagsProto: cfg.FeatureFlagsProto,
		sessionPools:      make(map[uint64]*managedSessionPool),
	}
	// stub == nil only happens on the test-only newSessionClientFromParts
	// path (unit tests wiring a fake ChannelPool without a real gRPC stub).
	// In that mode we do NOT construct a ClientConfigurationManager, do
	// NOT poll GetClientConfiguration, and do NOT fall back to any
	// default config — pools open with whatever seed the test supplies,
	// and SessionLoadListener never fires. Production NewClient always
	// supplies a stub.
	if stub != nil {
		sc.configManager = btransport.NewClientConfigurationManager(
			stub, sc.fullInstanceName(), cfg.AppProfile, cfg.ConfigMD, nil,
		)
		sc.configManager.Start(cfg.BackgroundCtx)
		if cfg.SessionLoadListener != nil {
			sc.configManager.AddSessionLoadListener(cfg.SessionLoadListener)
		}
	}
	return sc
}

func (sc *sessionClient) MeterProvider() otelmetric.MeterProvider {
	if sc.metricsFactory == nil {
		return nil
	}
	return sc.metricsFactory.OtelMeterProvider
}

// AddSessionLoadListener forwards to the internal
// ClientConfigurationManager. Returns a no-op unregister when no
// manager is wired (stub == nil at construction).
func (sc *sessionClient) AddSessionLoadListener(fn func(load float64)) func() {
	if sc.configManager == nil {
		return func() {}
	}
	return sc.configManager.AddSessionLoadListener(fn)
}

// MetricsFactory returns the *metrics.Factory the Client was
// constructed with. Exposed to internal callers (sessionTable) that
// need to construct tracers lazily when ctx doesn't already carry one.
func (sc *sessionClient) MetricsFactory() *metrics.Factory {
	return sc.metricsFactory
}

// OpenTable returns a TableAPI for a standard table.
func (sc *sessionClient) OpenTable(tableID string) TableAPI {
	fullName := sc.fullTableName(tableID)
	streamFactory := func(ctx context.Context) (btransport.Stream, error) { return sc.stub.OpenTable(ctx) }
	resource := "table:" + tableID
	readOwner := &poolCloser{}
	writeOwner := &poolCloser{}
	openRead := sc.buildLazyOpener(fullName, btransport.TABLE_SESSION, streamFactory,
		&btpb.OpenTableRequest{TableName: fullName, AppProfileId: sc.cfg.AppProfile, Permission: btpb.OpenTableRequest_PERMISSION_READ},
		poolKey{resource, permissionRead}, readOwner)
	openWrite := sc.buildLazyOpener(fullName, btransport.TABLE_SESSION, streamFactory,
		&btpb.OpenTableRequest{TableName: fullName, AppProfileId: sc.cfg.AppProfile, Permission: btpb.OpenTableRequest_PERMISSION_WRITE},
		poolKey{resource, permissionWrite}, writeOwner)
	return newSessionTable(tableID, openRead, openWrite, readOwner.Close, writeOwner.Close, btransport.READ_ROW, btransport.MUTATE_ROW, sc.perResourceMetadata(fullName, "table_name", fullName), sc.metricsFactory)
}

// OpenAuthorizedView returns a TableAPI for an authorized view.
func (sc *sessionClient) OpenAuthorizedView(table, view string) TableAPI {
	fullName := sc.fullAuthorizedViewName(table, view)
	streamFactory := func(ctx context.Context) (btransport.Stream, error) { return sc.stub.OpenAuthorizedView(ctx) }
	resource := fmt.Sprintf("av:%s:%s", table, view)
	readOwner := &poolCloser{}
	writeOwner := &poolCloser{}
	openRead := sc.buildLazyOpener(fullName, btransport.AUTHORIZED_VIEW_SESSION, streamFactory,
		&btpb.OpenAuthorizedViewRequest{AuthorizedViewName: fullName, AppProfileId: sc.cfg.AppProfile, Permission: btpb.OpenAuthorizedViewRequest_PERMISSION_READ},
		poolKey{resource, permissionRead}, readOwner)
	openWrite := sc.buildLazyOpener(fullName, btransport.AUTHORIZED_VIEW_SESSION, streamFactory,
		&btpb.OpenAuthorizedViewRequest{AuthorizedViewName: fullName, AppProfileId: sc.cfg.AppProfile, Permission: btpb.OpenAuthorizedViewRequest_PERMISSION_WRITE},
		poolKey{resource, permissionWrite}, writeOwner)
	return newSessionTable(table, openRead, openWrite, readOwner.Close, writeOwner.Close, btransport.READ_ROW_AUTH_VIEW, btransport.MUTATE_ROW_AUTH_VIEW, sc.perResourceMetadata(fullName, "authorized_view_name", fullName), sc.metricsFactory)
}

// OpenMaterializedView returns a read-only TableAPI for a
// materialized view. Only a read pool is opened; MutateRow errors
// cleanly via the nil openWrite passed to newSessionTable.
func (sc *sessionClient) OpenMaterializedView(view string) TableAPI {
	fullName := sc.fullMaterializedViewName(view)
	readOwner := &poolCloser{}
	openRead := sc.buildLazyOpener(fullName, btransport.MATERIALIZED_VIEW_SESSION,
		func(ctx context.Context) (btransport.Stream, error) { return sc.stub.OpenMaterializedView(ctx) },
		&btpb.OpenMaterializedViewRequest{
			MaterializedViewName: fullName,
			AppProfileId:         sc.cfg.AppProfile,
			Permission:           btpb.OpenMaterializedViewRequest_PERMISSION_READ,
		},
		poolKey{"mv:" + view, permissionRead}, readOwner)
	return newSessionTable("", openRead, nil, readOwner.Close, nil, btransport.READ_ROW_MAT_VIEW, nil, sc.perResourceMetadata(fullName, "materialized_view_name", fullName), sc.metricsFactory)
}

// Close tears down in a phased order that keeps late callbacks from
// firing against half-dead pools:
//  1. Stop config polling — no more UpdateConfig can fire after.
//  2. Close every session pool (per-pool listeners already detached).
//  3. Cancel the background ctx we constructed (unwinds the
//     client-scoped loops parented on it).
//  4. managed.Close() stops the DynamicScaleMonitor + ConnectionRecycler
//     wired by btransport.CreateAndStartManagedChannelPool, then closes
//     the underlying pool — in that order, so no scale-up / recycle tick
//     races against pool teardown. Test-fake path (managed.Pool == nil)
//     falls back to sc.channelPool.Close().
//  5. Shut down the metrics factory (final flush).
func (sc *sessionClient) Close() error {
	// Snapshot everything owned under the lock, then release before
	// running the actual Close/Shutdown/Cancel calls. Any of those can
	// block on a graceful drain or a final metrics flush, and any
	// concurrent caller reaching for a snapshot method (SessionDebug,
	// PoolSnapshots, ChannelPool) would deadlock if we held sessionPoolsMu
	// through the teardown.
	sc.sessionPoolsMu.Lock()
	pools := sc.sessionPools
	sc.sessionPools = nil // refuse subsequent Opens; createSessionPool nil-checks
	mgr := sc.configManager
	chp := sc.channelPool
	managed := sc.managedChannelPool
	factory := sc.metricsFactory
	cancel := sc.backgroundCancel
	sc.sessionPoolsMu.Unlock()

	if mgr != nil {
		mgr.Close()
	}
	for _, mp := range pools {
		if mp.unregister != nil {
			mp.unregister()
		}
		mp.pool.Close()
	}
	if cancel != nil {
		cancel()
	}
	// Prefer managed.Close (stops DSM + Recycler in order then closes
	// the pool). Test fakes bypass CreateAndStartManagedChannelPool so
	// managed.Pool is nil — fall back to the fake's own Close.
	var err error
	if managed.Pool != nil {
		err = managed.Close()
	} else if chp != nil {
		err = chp.Close()
	}
	if factory != nil && factory.Shutdown != nil {
		factory.Shutdown()
	}
	return err
}

// buildLazyOpener returns a closure that, on first invocation, creates a
// fresh pool for the given payload/key and hands its identity-scoped
// closer to owner. Returns nil when payload is nil (materialized-view
// write side).
//
// Each invocation mints its OWN pool (no keyed dedup) and records the
// teardown thunk on owner via set(). owner.Close — wired as the
// sessionTable's closeRead/closeWrite — then closes exactly that pool,
// by identity. This is the single-ownership contract: a sibling
// sessionTable sharing the same poolKey holds a different owner and a
// different pool, so its teardown can never close ours.
func (sc *sessionClient) buildLazyOpener(
	resourceName string,
	sessionDesc *btransport.SessionDescriptor,
	streamFactory func(ctx context.Context) (btransport.Stream, error),
	payload proto.Message,
	key poolKey,
	owner *poolCloser,
) func() (Invoker, error) {
	if payload == nil {
		return nil
	}
	return func() (Invoker, error) {
		pool, release, err := sc.createSessionPoolForPayload(resourceName, sessionDesc, streamFactory, payload, key)
		if err != nil {
			return nil, err
		}
		if pool == nil {
			// createSessionPool returns nil ONLY when Close() has already
			// snapshotted the pool set. Surface a distinct sentinel so
			// callers can tell "client closed" apart from "resource has
			// no write pool" (ErrWriteNotSupported) or "bookkeeping
			// drift" (errReadPoolNil).
			return nil, ErrClientClosed
		}
		// Hand the pool's teardown to the owner. If the sessionTable was
		// closed while we were dialing, set() tears the fresh pool down
		// immediately (it would otherwise leak, since owner.Close already
		// fired against an empty owner) and we surface client-closed —
		// joining any teardown error so a failed drain isn't swallowed.
		if closed, err := owner.set(release); closed {
			return nil, errors.Join(ErrClientClosed, err)
		}
		return pool, nil
	}
}

// createSessionPoolForPayload marshals the resource-typed OpenXxxRequest
// into the transport-level OpenSessionRequest envelope, builds routing
// metadata via the descriptor's MetadataFn, and delegates to
// createSessionPool. Returns the pool plus its identity-scoped release
// thunk (nil pool+release when Close already snapshotted the pool set).
func (sc *sessionClient) createSessionPoolForPayload(
	resourceName string,
	sessionDesc *btransport.SessionDescriptor,
	streamFactory func(ctx context.Context) (btransport.Stream, error),
	payload proto.Message,
	key poolKey,
) (*btransport.SessionPoolImpl, func() error, error) {
	if payload == nil {
		return nil, nil, nil
	}
	payloadBytes, err := proto.Marshal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("proto.Marshal session payload: %w", err)
	}
	handshake := &btpb.OpenSessionRequest{
		ProtocolVersion: sessionProtocolVersion,
		Payload:         payloadBytes,
		Flags:           sc.featureFlags(),
	}

	metaMap := sessionDesc.MetadataFn(payload)
	keys := make([]string, 0, len(metaMap))
	for k := range metaMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sessionMetadata := make([]string, 0, len(keys))
	for _, k := range keys {
		sessionMetadata = append(sessionMetadata, fmt.Sprintf("%s=%s", k, url.QueryEscape(metaMap[k])))
	}
	paramsVal := strings.Join(sessionMetadata, "&")

	md := metadata.Join(metadata.Pairs(
		resourcePrefixHeader, resourceName,
		requestParamsHeader, paramsVal,
	), sc.cfg.FeatureFlagsMD)

	pool, release := sc.createSessionPool(key, streamFactory, handshake, md, sessionDesc.Type)
	return pool, release, nil
}

// createSessionPool constructs a fresh pool, registers it in the
// observational index under a monotonic id, wires the config listener +
// background loops, and returns the pool along with an id-scoped release
// thunk that removes it from the index and closes it.
//
// Unlike the old getOrCreateSessionPool this NEVER dedups on key: every
// Open* call that reaches its opener gets its own pool. Ownership is
// carried by the returned closer (captured on a poolCloser by
// buildLazyOpener), so a TableCache loser discarding its handle, or an
// evicted-then-reopened handle, only ever closes the pool IT created —
// never a live sibling's. Returns (nil, nil) when Close() has already
// snapshotted+nil'd the pool set.
func (sc *sessionClient) createSessionPool(
	key poolKey,
	streamFactory func(ctx context.Context) (btransport.Stream, error),
	openSessionRequest *btpb.OpenSessionRequest,
	md metadata.MD,
	sessionType btransport.SessionType,
) (*btransport.SessionPoolImpl, func() error) {
	sc.sessionPoolsMu.Lock()
	// Close() sets pools=nil to refuse subsequent Opens; a nil map here
	// means teardown has already snapshotted the pool set.
	if sc.sessionPools == nil {
		sc.sessionPoolsMu.Unlock()
		return nil, nil
	}
	id := sc.nextPoolID.Add(1)
	// poolName is stamped as the `session_name` OTel metric label and
	// surfaces in sessionz — "<resource-id>-<PERM>" (see poolKey.displayName).
	// Several pools may share a name across successive Open* calls; the
	// numeric id (on SessionPoolImpl.poolID and this index) disambiguates
	// them for the sessionz ↔ channelz reverse link and per-session log
	// names while `session_name` cardinality stays bounded.
	poolName := key.displayName()
	pool := btransport.NewSessionPoolImpl(
		id,
		poolName, streamFactory, openSessionRequest, md, sessionType,
		sc.enableDebug,
	)
	mp := &managedSessionPool{id: id, key: key, pool: pool}
	sc.sessionPools[id] = mp
	configManager := sc.configManager
	sc.sessionPoolsMu.Unlock()

	if configManager != nil {
		unregister := configManager.AddSessionPoolListener(func(config *btpb.SessionClientConfiguration_SessionPoolConfiguration) {
			pool.UpdateConfig(config)
		})
		sc.sessionPoolsMu.Lock()
		if cur, stillThere := sc.sessionPools[id]; stillThere && cur == mp {
			mp.unregister = unregister
			sc.sessionPoolsMu.Unlock()
		} else {
			sc.sessionPoolsMu.Unlock()
			unregister()
		}
	}

	pool.Start()
	return pool, sc.releasePoolByID(id)
}

// releasePoolByID returns the identity-scoped teardown thunk for the
// pool registered under id. The thunk removes the pool from the
// observational index and closes it, and is a no-op when:
//   - the client has already Closed (sessionPools == nil), so a late
//     release draining out of a TableCache handle doesn't double-close a
//     pool Client.Close is already tearing down; or
//   - the id is absent (already released — makes the poolCloser's
//     single-shot Close doubly safe).
//
// Teardown runs OUTSIDE sessionPoolsMu so a graceful pool drain (which
// can block on in-flight vRPCs) doesn't deadlock a snapshotter waiting
// on the lock — the snapshot-under-lock / teardown-outside pattern also
// used by Client.Close.
func (sc *sessionClient) releasePoolByID(id uint64) func() error {
	return func() error {
		sc.sessionPoolsMu.Lock()
		if sc.sessionPools == nil {
			sc.sessionPoolsMu.Unlock()
			return nil
		}
		mp, ok := sc.sessionPools[id]
		if !ok {
			sc.sessionPoolsMu.Unlock()
			return nil
		}
		delete(sc.sessionPools, id)
		sc.sessionPoolsMu.Unlock()

		if mp.unregister != nil {
			mp.unregister()
		}
		return mp.pool.Close()
	}
}

// featureFlags returns the FeatureFlags proto stamped onto every
// OpenSessionRequest.Flags. Constructed once at NewClient time from
// the same input as the bigtable-features gRPC header so the two are
// byte-identical — the server rejects OpenSession with INVALID_ARGUMENT
// when they disagree on session-mode flags.
func (sc *sessionClient) featureFlags() *btpb.FeatureFlags {
	return sc.featureFlagsProto
}

// perResourceMetadata builds the per-vRPC context metadata: the pair
// carried on the outgoing gRPC call for the underlying bidi stream.
// Header shape matches classic Table.md (bigtable/open.go:32-35).
func (sc *sessionClient) perResourceMetadata(fullResourceName, paramKey, paramVal string) metadata.MD {
	return metadata.Join(metadata.Pairs(
		resourcePrefixHeader, fullResourceName,
		requestParamsHeader, fmt.Sprintf("%s=%s&app_profile_id=%s", paramKey, url.QueryEscape(paramVal), url.QueryEscape(sc.cfg.AppProfile)),
	), sc.cfg.FeatureFlagsMD)
}

// buildConfigMetadata builds the metadata attached to every
// GetClientConfiguration RPC — both the DirectAccessChecker's compat
// probe and ClientConfigurationManager's steady-state polls. The proto
// field is instance_name, and Java pairs it with app_profile_id in
// x-goog-request-params so the RLS router can key on both
// (Util.composeMetadata + ClientConfigurationManager ctor).
func buildConfigMetadata(fullInstance, appProfile string, featureFlagsMD metadata.MD) metadata.MD {
	return metadata.Join(metadata.Pairs(
		resourcePrefixHeader, fullInstance,
		requestParamsHeader, fmt.Sprintf("instance_name=%s&app_profile_id=%s",
			url.QueryEscape(fullInstance), url.QueryEscape(appProfile)),
	), featureFlagsMD)
}

// Resource-name composition — duplicated from bigtable.Client to
// avoid an import cycle. Keep in sync with client.go's
// fullTableName / fullAuthorizedViewName / fullMaterializedViewName /
// fullInstanceName helpers.

func (sc *sessionClient) fullTableName(table string) string {
	return fmt.Sprintf("projects/%s/instances/%s/tables/%s", sc.cfg.Project, sc.cfg.Instance, table)
}

func (sc *sessionClient) fullAuthorizedViewName(table, view string) string {
	return fmt.Sprintf("projects/%s/instances/%s/tables/%s/authorizedViews/%s", sc.cfg.Project, sc.cfg.Instance, table, view)
}

func (sc *sessionClient) fullMaterializedViewName(view string) string {
	return fmt.Sprintf("projects/%s/instances/%s/materializedViews/%s", sc.cfg.Project, sc.cfg.Instance, view)
}

func (sc *sessionClient) fullInstanceName() string {
	return fmt.Sprintf("projects/%s/instances/%s", sc.cfg.Project, sc.cfg.Instance)
}

// Debug accessors exposed for the bigtable/session_debug.go providers.
// Kept off the Client interface — consumers who need them
// type-assert to *sessionClient.

// ConfigManager returns the internal ClientConfigurationManager for
// configz. Nil when no stub was provided.
func (sc *sessionClient) ConfigManager() *btransport.ClientConfigurationManager {
	return sc.configManager
}

// PoolSnapshots returns one PoolSnapshot per owned pool, ordered by
// pool key for stable rendering. Same lock discipline as
// SessionManager.ManagerSnapshot.
func (sc *sessionClient) PoolSnapshots() []btransport.PoolSnapshot {
	entries := sc.orderedPoolEntries()
	out := make([]btransport.PoolSnapshot, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.pool.PoolSnapshot())
	}
	return out
}

// LoadBalancingSnapshots returns per-pool picker + pick-history
// snapshots for loadz. Ordered by pool key.
func (sc *sessionClient) LoadBalancingSnapshots() []btransport.LoadBalancingSnapshot {
	entries := sc.orderedPoolEntries()
	out := make([]btransport.LoadBalancingSnapshot, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.pool.LoadBalancingSnapshot())
	}
	return out
}

// poolEntry is the internal (id, key, pool) tuple used by snapshot
// methods so they can sort by poolKey without duplicating the collection
// loop. id breaks ties when two live pools share a poolKey (successive
// Open* calls for the same resource) so ordering stays stable.
type poolEntry struct {
	id   uint64
	key  poolKey
	pool *btransport.SessionPoolImpl
}

// orderedPoolEntries snapshots the pools map under lock and returns
// its non-nil entries sorted by poolKey, then id.
func (sc *sessionClient) orderedPoolEntries() []poolEntry {
	sc.sessionPoolsMu.Lock()
	entries := make([]poolEntry, 0, len(sc.sessionPools))
	for _, mp := range sc.sessionPools {
		if mp == nil || mp.pool == nil {
			continue
		}
		entries = append(entries, poolEntry{id: mp.id, key: mp.key, pool: mp.pool})
	}
	sc.sessionPoolsMu.Unlock()
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].key != entries[j].key {
			return entries[i].key.less(entries[j].key)
		}
		return entries[i].id < entries[j].id
	})
	return entries
}

// ChannelPool returns the *btransport.BigtableChannelPool the
// sessionClient was constructed with, if any. Used by channelz to
// surface session-pool channel stats without leaking the interface
// through the public Client API.
func (sc *sessionClient) ChannelPool() *btransport.BigtableChannelPool {
	if sc.channelPool == nil {
		return nil
	}
	bp, _ := sc.channelPool.(*btransport.BigtableChannelPool)
	return bp
}
