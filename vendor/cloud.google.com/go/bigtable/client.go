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

package bigtable

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"time"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	metrics "cloud.google.com/go/bigtable/internal/metrics"
	btopt "cloud.google.com/go/bigtable/internal/option"
	"cloud.google.com/go/bigtable/internal/session"
	btransport "cloud.google.com/go/bigtable/internal/transport"
	"cloud.google.com/go/internal/trace"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"
	gtransport "google.golang.org/api/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const directAccessEnvVar = "CBT_ENABLE_DIRECTPATH"

// Client is a client for reading and writing data to tables in an instance.
//
// A Client is safe to use concurrently, except for its Close method.
type Client struct {
	connPool                gtransport.ConnPool
	client                  btpb.BigtableClient
	project, instance       string
	appProfile              string
	metricsTracerFactory    *metrics.Factory
	retryOption             gax.CallOption
	executeQueryRetryOption gax.CallOption
	featureFlagsMD          metadata.MD // Pre-computed feature flags metadata to be sent with each request.
	mPool                   btransport.ManagedChannelPool
	// diverter picks between the classic and session data path on every
	// Open* return. Initialized with sessionLoad=0.0 so all traffic
	// stays on the classic path until the session backend's
	// ConfigurationManager bumps the ratio via AddSessionLoadListener.
	diverter *btransport.Diverter
	// sessionImpl is the session data-plane client. Constructed by
	// NewClientWithConfig by default so a control-plane session-load
	// bump can route traffic to it without a client restart. Nil when
	// the caller pre-dialed a custom gRPC conn or opted out via
	// ClientConfig.DisableSession — every downstream reader
	// (open.go's getOrCreateSession*, Client.Close) nil-guards. When
	// non-nil, no RPCs actually travel to the session backend until
	// the diverter's SessionLoad > 0 — the initial value is 0.0 and
	// only the server-driven ClientConfigurationManager writes to it.
	// Session pools + streams are lazily materialized on first use,
	// so an idle client pays only for one channel pool and one
	// config-poll goroutine.
	sessionImpl session.Client
	// sessionTables caches per-resource session.TableAPI handles so
	// repeat Open* calls return the same handle (and by extension the
	// same underlying session pools). session.Client does not cache;
	// this Client is responsible. Entries evict on TTL-idle (default
	// 1 h) via a background sweeper, or immediately when the caller
	// calls Close() on the returned handle. See
	// internal/session/table_cache.go.
	sessionTables *session.TableCache
}

// ClientConfig has configurations for the client.
type ClientConfig struct {
	// The id of the app profile to associate with all data operations sent from this client.
	// If unspecified, the default app profile for the instance will be used.
	AppProfile string

	// MetricsProvider controls the built-in client-side metrics.
	//
	// Leave unset (nil) or set to DefaultMetricsProvider{} to enable the
	// built-in Cloud Monitoring exporter (default behavior). Set to
	// NoopMetricsProvider{} to disable metrics entirely.
	//
	// TODO: support user provided meter provider
	MetricsProvider MetricsProvider

	// DisableDynamicChannelPool disables the dynamic channel resizing based on load
	// Dynamic channel resizing  is enabled by default to resize based on load and avoid queuing of requests.
	DisableDynamicChannelPool bool

	// DisableConnectionRecycler disables the automatic preemptive refresh of connection.
	// Preemptive connection is default to true
	DisableConnectionRecycler bool

	// DisableDirectAccess disables direct access by default.
	DisableDirectAccess bool

	// DisableSession, when true, tells NewClientWithConfig to skip
	// constructing the session data-plane client entirely. sessionImpl
	// and sessionTables are left nil; the client runs classic-only and
	// pays none of the session infrastructure's per-connection background
	// goroutines or gRPC-channel cost.
	//
	// Effect on Table: Open() returns a *Table whose divertible field is
	// still built (Diverter is a classic-side concern), but its session
	// side is nil, so TableShim.useSession() reports false and every
	// Apply / ReadRow routes to the classic path unconditionally.
	//
	// Intended for callers who have specific reasons to opt out of the
	// session data plane — running against a backend that doesn't
	// support it, benchmarking classic-only baselines, or resource-
	// constrained environments where the +N goroutines + +MB RSS of an
	// idle session pool matter. Default (false) keeps the current
	// behavior: session client is always constructed.
	DisableSession bool
}

// MetricsProvider is a wrapper for the built-in metrics meter provider.
// Type alias to the internal metrics package's interface — callers keep
// using bigtable.MetricsProvider while the implementation lives in
// bigtable/internal/metrics so both classic and session data planes can
// share it without an import cycle.
type MetricsProvider = metrics.MetricsProvider

// DefaultMetricsProvider enables the built-in Cloud Monitoring metrics
// exporter (the same behavior as leaving ClientConfig.MetricsProvider
// nil). Type alias to the internal metrics package's implementation.
type DefaultMetricsProvider = metrics.DefaultMetricsProvider

// NoopMetricsProvider disables the built-in metrics. Type alias to the
// internal metrics package's implementation.
type NoopMetricsProvider = metrics.NoopMetricsProvider

// NewClient creates a new Client for a given project and instance.
// The default ClientConfig will be used.
func NewClient(ctx context.Context, project, instance string, opts ...option.ClientOption) (*Client, error) {
	return NewClientWithConfig(ctx, project, instance, ClientConfig{}, opts...)
}

// NewClientWithConfig creates a new client with the given config.
func NewClientWithConfig(ctx context.Context, project, instance string, config ClientConfig, opts ...option.ClientOption) (*Client, error) {
	clientCreationTimestamp := time.Now()
	metricsProvider := config.MetricsProvider
	if emulatorAddr := os.Getenv("BIGTABLE_EMULATOR_HOST"); emulatorAddr != "" {
		// Do not emit metrics when emulator is being used
		metricsProvider = NoopMetricsProvider{}
	}

	// Create a OpenTelemetry metrics configuration
	metricsTracerFactory, err := metrics.NewFactory(ctx, project, instance, config.AppProfile, "", metricsProvider, opts...)
	if err != nil {
		return nil, err
	}

	o, err := btopt.DefaultClientOptions(prodAddr, mtlsProdAddr, Scope, clientUserAgent)
	if err != nil {
		return nil, err
	}
	// for otel metrics
	if metricsTracerFactory.Enabled {
		if len(metricsTracerFactory.ClientOpts) > 0 {
			o = append(o, metricsTracerFactory.ClientOpts...)
		}
	}

	// Add gRPC client interceptors to supply Google client information. No external interceptors are passed.
	o = append(o, btopt.ClientInterceptorOptions(nil, nil)...)
	o = append(o, option.WithGRPCDialOption(grpc.WithStatsHandler(metrics.SharedStatsHandler)))
	// Default to a connection pool that can be overridden. Raised from 4 to
	// defaultBigtableConnPoolSize to compensate for dynamic channel pool
	// scaling being disabled by default
	// (see https://github.com/googleapis/google-cloud-go/issues/14582).
	o = append(o,
		option.WithGRPCConnectionPool(defaultBigtableConnPoolSize),
		// Set the max size to correspond to server-side limits.
		option.WithGRPCDialOption(grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(1<<28), grpc.MaxCallRecvMsgSize(1<<28))),
	)

	// DirectAccess opt-set is centralized in btopt so the classic
	// client, the DirectAccess probe (direct_access_check.go), and the
	// session client (internal/session) cannot drift on which options
	// are applied to a DirectPath dial.
	directAccessOptions := btopt.DirectAccessOptions()

	o = append(o, opts...)
	o = append(o, internaloption.EnableNewAuthLibrary())
	o = append(o, internaloption.EnableJwtWithScope())

	// RetryInfo is unconditionally on; retryer + on-wire flag agree via
	// NewFeatureFlagsProto (feature_flags.go) and defaultRetryOption.
	retryOption := defaultRetryOption
	executeQueryRetryOption := defaultExecuteQueryRetryOption

	// Build feature-flags proto + header MD ONCE. Both are threaded
	// into session.NewClient below so classic and session ship
	// byte-identical bigtable-features headers AND session's
	// OpenSessionRequest.Flags reuses the same proto reference —
	// zero drift by construction, no re-marshal in session.
	featureFlagsProto := btransport.NewFeatureFlagsProto(btransport.FeatureFlagsInput{
		ClientSideMetricsEnabled: metricsTracerFactory.Enabled,
		EnableDirectAccess:       isDirectAccessEnabled(config),
	})
	directAccessMD := btransport.MarshalFeatureFlagsMD(featureFlagsProto)

	var mPool btransport.ManagedChannelPool
	enableBigtableConnPool := btopt.EnableBigtableConnectionPool()
	grpcConnOptType := reflect.TypeOf(option.WithGRPCConn(nil))
	for _, opt := range opts {
		if reflect.TypeOf(opt) == grpcConnOptType {
			enableBigtableConnPool = false
			break
		}
	}
	if !enableBigtableConnPool {
		// Use the regular ConnPool
		// For regular ConnPool the Direct Access is off by default so we need to check the env var again.
		if enabled, _ := strconv.ParseBool(os.Getenv(directAccessEnvVar)); enabled {
			o = append(o, directAccessOptions...)
		}
	}

	poolConfig := btransport.ChannelPoolConfig{
		AppProfile:                config.AppProfile,
		DisableDynamicChannelPool: config.DisableDynamicChannelPool,
		DisableConnectionRecycler: config.DisableConnectionRecycler,
		DisableDirectAccess:       config.DisableDirectAccess,
	}

	mPool, err = btransport.CreateAndStartManagedChannelPool(
		ctx,
		project,
		instance,
		poolConfig,
		metricsTracerFactory.OtelMeterProvider,
		o,
		directAccessOptions,
		directAccessMD,
		clientCreationTimestamp,
		enableBigtableConnPool,
	)
	if err != nil {
		return nil, err
	}

	c := &Client{
		connPool:                mPool.Pool,
		client:                  btpb.NewBigtableClient(mPool.Pool),
		project:                 project,
		instance:                instance,
		appProfile:              config.AppProfile,
		metricsTracerFactory:    metricsTracerFactory,
		retryOption:             retryOption,
		executeQueryRetryOption: executeQueryRetryOption,
		featureFlagsMD:          directAccessMD,
		mPool:                   mPool,
		diverter:                btransport.NewDiverter(0.0),
	}

	// Session data-plane backend construction has two guardrails so it
	// can't interfere with the classic path in test / emulator setups:
	//
	//  1. If the caller passed option.WithGRPCConn(conn) — i.e. handed
	//     us one pre-dialed *grpc.ClientConn to use for everything —
	//     skip session entirely. Two independent backends can't
	//     reasonably share one physical conn; running the session
	//     dialer against a pre-dialed conn either gets both backends
	//     entangled on the same underlying transport (so a
	//     session-side teardown propagates a "connection is closing"
	//     error to classic RPCs — the conformance test-proxy failure
	//     mode) or double-dials the same fake server. Same guard
	//     covers BIGTABLE_EMULATOR_HOST since DefaultClientOptions
	//     sets WithGRPCConn internally in that mode.
	//
	//  2. On session.NewClient failure, tear down the classic pool
	//     since we won't return c to the caller.
	//
	// When both guardrails pass, wire AddSessionLoadListener so the
	// server-driven SessionLoad from ClientConfigurationManager retargets
	// traffic through this Client's Diverter — a control-plane update
	// then shifts traffic across every open TableShim without a client
	// restart.
	// Inspect the merged option list (o), not the raw caller opts.
	// BIGTABLE_EMULATOR_HOST injects option.WithGRPCConn inside
	// btopt.DefaultClientOptions (option.go:113), so it lives in o —
	// callers never pass it explicitly. Reading only from opts misses
	// the emulator conn and lets session.NewClient dial an empty
	// resolver target (fails with "passthrough: received empty target
	// in Build()"), breaking every emulator-based test.
	preDialed := false
	if uResolver, resErr := internaloption.NewUnsafeResolver(o...); resErr == nil {
		preDialed = uResolver.ResolvedGRPCConnIsCustom()
	}
	// Skip session-client construction when either (a) caller pre-dialed
	// a custom conn (session.NewClient can't dial through it) or (b)
	// caller opted out via ClientConfig.DisableSession. In both cases
	// sessionImpl + sessionTables stay nil; every downstream caller
	// (open.go's getOrCreateSession*, Client.Close) already nil-guards
	// on that state.
	if !preDialed && !config.DisableSession {
		// Pass the fully-merged option list (o), not the raw caller
		// opts. gtransport.Dial needs the DefaultClientOptions merged
		// in (endpoint, scopes, user-agent, interceptors) — passing
		// bare opts leaves the resolver target empty and the dial
		// aborts with "passthrough: received empty target in Build()".
		sc, sessionErr := session.NewClient(ctx, project, instance, config.AppProfile, "", metricsProvider, featureFlagsProto, o...)
		if sessionErr != nil {
			// Best-effort cleanup of the classic pool since we won't
			// return c to the caller. Go through the ManagedChannelPool
			// wrapper so any wrapper-owned cleanup — metrics reporter,
			// connection recycler, dynamic scale monitor — winds down
			// too. Matches (*Client).Close.
			_ = mPool.Close()
			return nil, fmt.Errorf("bigtable: session.NewClient: %w", sessionErr)
		}
		sc.AddSessionLoadListener(c.diverter.SetSessionLoad)
		c.sessionImpl = sc

		// Per-resource TableAPI cache with TTL-on-idle eviction. The
		// cache is opener-agnostic — each getOrCreateSession* helper
		// in open.go passes its own openFn per call, using the fully-
		// qualified resource name as the cache key. Only constructed
		// when the session backend is actually wired — a sweeper
		// goroutine over an always-empty map would be dead weight.
		c.sessionTables = session.NewTableCache(session.DefaultTableCacheTTL, session.DefaultTableCacheSweepInterval, nil /* time.Now */)
	}

	return c, nil
}

// Close closes the Client.
func (c *Client) Close() error {
	if c.metricsTracerFactory != nil {
		c.metricsTracerFactory.Shutdown()
	}
	// Close the per-resource cache first so its sweeper goroutine
	// stops and every cached handle sees Close() before we tear down
	// the session client that owns their pools. sessionTables is nil
	// on hand-built Clients that skipped session-backend wiring; the
	// cache's own Close() nil-checks for that.
	c.sessionTables.Close()
	// Then the session backend — its bookkeeping (session pools,
	// ConfigurationManager poller) winds down before we drop the
	// shared gRPC channels. Any error is aggregated with the classic
	// pool's Close error.
	var sessionErr error
	if c.sessionImpl != nil {
		sessionErr = c.sessionImpl.Close()
	}
	if err := c.mPool.Close(); err != nil {
		if sessionErr != nil {
			return fmt.Errorf("bigtable.Client.Close: classic pool: %w; session: %v", err, sessionErr)
		}
		return err
	}
	return sessionErr
}

func (c *Client) fullInstanceName() string {
	return fmt.Sprintf("projects/%s/instances/%s", c.project, c.instance)
}

func (c *Client) fullTableName(table string) string {
	return fmt.Sprintf("projects/%s/instances/%s/tables/%s", c.project, c.instance, table)
}

func (c *Client) fullAuthorizedViewName(table string, authorizedView string) string {
	return fmt.Sprintf("projects/%s/instances/%s/tables/%s/authorizedViews/%s", c.project, c.instance, table, authorizedView)
}

func (c *Client) fullMaterializedViewName(materializedView string) string {
	return fmt.Sprintf("projects/%s/instances/%s/materializedViews/%s", c.project, c.instance, materializedView)
}

func (c *Client) reqParamsHeaderValTable(table string) string {
	return fmt.Sprintf("table_name=%s&app_profile_id=%s", url.QueryEscape(c.fullTableName(table)), url.QueryEscape(c.appProfile))
}

func (c *Client) reqParamsHeaderValInstance() string {
	return fmt.Sprintf("name=%s&app_profile_id=%s", url.QueryEscape(c.fullInstanceName()), url.QueryEscape(c.appProfile))
}

// PingAndWarm pings the server and warms up the connection.
func (c *Client) PingAndWarm(ctx context.Context) (err error) {
	md := metadata.Join(metadata.Pairs(
		resourcePrefixHeader, c.fullInstanceName(),
		requestParamsHeader, c.reqParamsHeaderValInstance(),
	), c.featureFlagsMD)

	ctx = mergeOutgoingMetadata(ctx, md)
	ctx = trace.StartSpan(ctx, "cloud.google.com/go/bigtable/PingAndWarm")
	defer func() { trace.EndSpan(ctx, err) }()
	mt := c.newBuiltinMetricsTracer(ctx, "", false)
	defer mt.RecordOperationCompletion()
	ctx = metrics.NewContext(ctx, mt)

	err = c.pingerWithMetadata(ctx)
	statusCode, statusErr := metrics.ConvertToGrpcStatusErr(err)
	mt.SetCurrOpStatus(statusCode)
	return statusErr
}

func (c *Client) pingerWithMetadata(ctx context.Context) (err error) {
	req := &btpb.PingAndWarmRequest{
		Name:         c.fullInstanceName(),
		AppProfileId: c.appProfile,
	}
	err = gaxInvokeWithRecorder(ctx, "PingAndWarm", func(ctx context.Context, headerMD, trailerMD *metadata.MD, _ gax.CallSettings) error {
		var err error
		_, err = c.client.PingAndWarm(ctx, req, grpc.Header(headerMD), grpc.Trailer(trailerMD))
		return err
	})

	return err

}

func (c *Client) newBuiltinMetricsTracer(ctx context.Context, table string, isStreaming bool) *metrics.Tracer {
	return c.metricsTracerFactory.CreateTracer(ctx, table, isStreaming)
}

// isDirectAccessEnabled resolves whether this client advertises
// DirectAccess capability on the wire AND dials DirectPath. Single
// source of truth threaded into both the classic and session
// FeatureFlags builders so header bits stay in lockstep with the
// dial-time switch. CBT_ENABLE_DIRECTPATH overrides
// ClientConfig.DisableDirectAccess when set; unset falls back to
// the config default.
func isDirectAccessEnabled(config ClientConfig) bool {
	if os.Getenv(directAccessEnvVar) == "" {
		return !config.DisableDirectAccess
	}
	res, _ := strconv.ParseBool(os.Getenv(directAccessEnvVar))
	return res
}
