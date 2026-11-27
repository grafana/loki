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
	"strconv"
	"time"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	btopt "cloud.google.com/go/bigtable/internal/option"
	btransport "cloud.google.com/go/bigtable/internal/transport"
	"cloud.google.com/go/internal/trace"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"
	gtransport "google.golang.org/api/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Client is a client for reading and writing data to tables in an instance.
//
// A Client is safe to use concurrently, except for its Close method.
type Client struct {
	connPool                gtransport.ConnPool
	client                  btpb.BigtableClient
	project, instance       string
	appProfile              string
	metricsTracerFactory    *builtinMetricsTracerFactory
	disableRetryInfo        bool
	retryOption             gax.CallOption
	executeQueryRetryOption gax.CallOption
	featureFlagsMD          metadata.MD // Pre-computed feature flags metadata to be sent with each request.
	dynamicScaleMonitor     *btransport.DynamicScaleMonitor
	connsRecycler           *btransport.ConnectionRecycler
}

// ClientConfig has configurations for the client.
type ClientConfig struct {
	// The id of the app profile to associate with all data operations sent from this client.
	// If unspecified, the default app profile for the instance will be used.
	AppProfile string

	// If not set or set to nil, client side metrics will be collected and exported
	//
	// To disable client side metrics, set 'MetricsProvider' to 'NoopMetricsProvider'
	//
	// TODO: support user provided meter provider
	MetricsProvider MetricsProvider

	// DisableDynamicChannelPool disables the dynamic channel resizing based on load
	// Dynamic channel resizing  is enabled by default to resize based on load and avoid queuing of requests.
	DisableDynamicChannelPool bool

	// DisableConnectionRecycler disables the automatic preemptive refresh of connection.
	// Preemptive connection is default to true
	DisableConnectionRecycler bool
}

// MetricsProvider is a wrapper for built in metrics meter provider
type MetricsProvider interface {
	isMetricsProvider()
}

// NoopMetricsProvider can be used to disable built in metrics
type NoopMetricsProvider struct{}

func (NoopMetricsProvider) isMetricsProvider() {}

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
	metricsTracerFactory, err := newBuiltinMetricsTracerFactory(ctx, project, instance, config.AppProfile, metricsProvider, opts...)
	if err != nil {
		return nil, err
	}

	o, err := btopt.DefaultClientOptions(prodAddr, mtlsProdAddr, Scope, clientUserAgent)
	if err != nil {
		return nil, err
	}
	// for otel metrics
	if metricsTracerFactory.enabled {
		if len(metricsTracerFactory.clientOpts) > 0 {
			o = append(o, metricsTracerFactory.clientOpts...)
		}
	}

	// Add gRPC client interceptors to supply Google client information. No external interceptors are passed.
	o = append(o, btopt.ClientInterceptorOptions(nil, nil)...)
	o = append(o, option.WithGRPCDialOption(grpc.WithStatsHandler(sharedLatencyStatsHandler)))
	// Default to a small connection pool that can be overridden.
	o = append(o,
		option.WithGRPCConnectionPool(4),
		// Set the max size to correspond to server-side limits.
		option.WithGRPCDialOption(grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(1<<28), grpc.MaxCallRecvMsgSize(1<<28))),
	)

	var directPathOptions = []option.ClientOption{
		internaloption.EnableDirectPath(true),
		internaloption.EnableDirectPathXds(),
	}

	// Allow non-default service account in DirectPath.
	o = append(o, internaloption.AllowNonDefaultServiceAccount(true))
	o = append(o, opts...)

	// TODO(b/372244283): Remove after b/358175516 has been fixed
	o = append(o, internaloption.EnableAsyncRefreshDryRun(metricsTracerFactory.newAsyncRefreshErrHandler()))

	disableRetryInfo := false

	// If DISABLE_RETRY_INFO=1, library does not base retry decision and back off time on server returned RetryInfo value.
	disableRetryInfoEnv := os.Getenv("DISABLE_RETRY_INFO")
	disableRetryInfo = disableRetryInfoEnv == "1"
	retryOption := defaultRetryOption
	executeQueryRetryOption := defaultExecuteQueryRetryOption
	if disableRetryInfo {
		retryOption = clientOnlyRetryOption
		executeQueryRetryOption = clientOnlyExecuteQueryRetryOption
	}

	// Create the feature flags metadata with direct access enabled
	// setting feature flags for direct access is good
	// as CFE/GFE will call RLS with gslb target type
	// only TD calls the RLS with grpc target type
	// and we evaluate the directAccess option after that.
	directAccessMD := createFeatureFlagsMD(metricsTracerFactory.enabled, disableRetryInfo, true)

	var connPool gtransport.ConnPool
	var connPoolErr error
	var dsm *btransport.DynamicScaleMonitor
	var connRecycler *btransport.ConnectionRecycler

	enableBigtableConnPool := btopt.EnableBigtableConnectionPool()
	var connPoolSize int
	if enableBigtableConnPool {
		uResolver, err := internaloption.NewUnsafeResolver(o...)
		if err != nil {
			// just fallback
			connPoolSize = defaultBigtableConnPoolSize
		}

		connPoolSize = uResolver.ResolvedGRPCConnPoolSize()
		// Fallback to 10 if it resolves to 0
		if connPoolSize == 0 {
			connPoolSize = defaultBigtableConnPoolSize
		}

		fullInstanceName := fmt.Sprintf("projects/%s/instances/%s", project, instance)

		directAccessDialerOptions := make([]option.ClientOption, len(o))
		copy(directAccessDialerOptions, o)
		directAccessDialerOptions = append(directAccessDialerOptions, directPathOptions...)
		// enable hard bound tokens by default
		directAccessDialerOptions = append(directAccessDialerOptions, internaloption.AllowHardBoundTokens("ALTS"))

		directAccessDialer := func() (*btransport.BigtableConn, error) {
			grpcConn, err := gtransport.Dial(ctx, directAccessDialerOptions...)
			if err != nil {
				return nil, err
			}
			return btransport.NewBigtableConn(grpcConn), nil
		}

		btPool, err := btransport.NewBigtableChannelPool(ctx,
			connPoolSize,
			btopt.BigtableLoadBalancingStrategy(),
			func() (*btransport.BigtableConn, error) {
				grpcConn, err := gtransport.Dial(ctx, o...)
				if err != nil {
					return nil, err
				}
				return btransport.NewBigtableConn(grpcConn), nil
			},
			clientCreationTimestamp,
			// options
			btransport.WithInstanceName(fullInstanceName),
			btransport.WithAppProfile(config.AppProfile),
			btransport.WithFeatureFlagsMetadata(directAccessMD),
			btransport.WithMetricsReporterConfig(btopt.DefaultMetricsReporterConfig()),
			btransport.WithMeterProvider(metricsTracerFactory.otelMeterProvider),
			btransport.WithDirectAccessFeatureFlagsMetadata(directAccessMD),
			btransport.WithDirectAccessDialer(directAccessDialer),
		)

		if err != nil {
			connPoolErr = err
		} else {
			connPool = btPool

			// Validate dynamic config early if enabled
			if !config.DisableDynamicChannelPool {
				if err := btransport.ValidateDynamicConfig(btopt.DefaultDynamicChannelPoolConfig(), defaultBigtableConnPoolSize); err != nil {
					return nil, fmt.Errorf("invalid DynamicChannelPoolConfig: %w", err)
				}

				dsm = btransport.NewDynamicScaleMonitor(btopt.DefaultDynamicChannelPoolConfig(), btPool)
				dsm.Start(ctx) // Start the monitor's background goroutine
			}
			// connection recyler.
			if !config.DisableConnectionRecycler {
				connRecycler = btransport.NewConnectionRecycler(btopt.DefaultConnectionRecycleConfig(), btPool)
				connRecycler.Start(ctx) // Start the monitor's background goroutine
			}

		}

	} else {
		enableDirectAccess, _ := strconv.ParseBool(os.Getenv("CBT_ENABLE_DIRECTPATH"))
		if enableDirectAccess {
			o = append(o, directPathOptions...)
			if disableBoundToken, _ := strconv.ParseBool(os.Getenv("CBT_DISABLE_DIRECTPATH_BOUND_TOKEN")); !disableBoundToken {
				o = append(o, internaloption.AllowHardBoundTokens("ALTS"))
			}
		}
		// use to regular ConnPool
		connPool, connPoolErr = gtransport.DialPool(ctx, o...)
	}

	if connPoolErr != nil {
		return nil, connPoolErr
	}

	return &Client{
		connPool:                connPool,
		client:                  btpb.NewBigtableClient(connPool),
		project:                 project,
		instance:                instance,
		appProfile:              config.AppProfile,
		metricsTracerFactory:    metricsTracerFactory,
		disableRetryInfo:        disableRetryInfo,
		retryOption:             retryOption,
		executeQueryRetryOption: executeQueryRetryOption,
		featureFlagsMD:          directAccessMD,
		dynamicScaleMonitor:     dsm,
		connsRecycler:           connRecycler,
	}, nil
}

// Close closes the Client.
func (c *Client) Close() error {
	if c.dynamicScaleMonitor != nil {
		c.dynamicScaleMonitor.Stop()
	}
	if c.metricsTracerFactory != nil {
		c.metricsTracerFactory.shutdown()
	}
	if c.connsRecycler != nil {
		c.connsRecycler.Stop()
	}
	return c.connPool.Close()
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

// Open opens a table.
func (c *Client) Open(table string) *Table {
	return &Table{
		c:     c,
		table: table,
		md: metadata.Join(metadata.Pairs(
			resourcePrefixHeader, c.fullTableName(table),
			requestParamsHeader, c.reqParamsHeaderValTable(table),
		), c.featureFlagsMD),
	}
}

// OpenTable opens a table.
func (c *Client) OpenTable(table string) TableAPI {
	return &tableImpl{Table{
		c:     c,
		table: table,
		md: metadata.Join(metadata.Pairs(
			resourcePrefixHeader, c.fullTableName(table),
			requestParamsHeader, c.reqParamsHeaderValTable(table),
		), c.featureFlagsMD),
	}}
}

// OpenAuthorizedView opens an authorized view.
func (c *Client) OpenAuthorizedView(table, authorizedView string) TableAPI {
	return &tableImpl{Table{
		c:     c,
		table: table,
		md: metadata.Join(metadata.Pairs(
			resourcePrefixHeader, c.fullAuthorizedViewName(table, authorizedView),
			requestParamsHeader, c.reqParamsHeaderValTable(table),
		), c.featureFlagsMD),
		authorizedView: authorizedView,
	}}
}

// OpenMaterializedView opens a materialized view.
func (c *Client) OpenMaterializedView(materializedView string) TableAPI {
	return &tableImpl{Table{
		c: c,
		md: metadata.Join(metadata.Pairs(
			resourcePrefixHeader, c.fullMaterializedViewName(materializedView),
			requestParamsHeader, c.reqParamsHeaderValTable(materializedView),
		), c.featureFlagsMD),
		materializedView: materializedView,
	}}
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
	defer mt.recordOperationCompletion()

	err = c.pingerWithMetadata(ctx, mt)
	statusCode, statusErr := convertToGrpcStatusErr(err)
	mt.currOp.setStatus(statusCode.String())
	return statusErr
}

func (c *Client) pingerWithMetadata(ctx context.Context, mt *builtinMetricsTracer) (err error) {
	req := &btpb.PingAndWarmRequest{
		Name:         c.fullInstanceName(),
		AppProfileId: c.appProfile,
	}
	err = gaxInvokeWithRecorder(ctx, mt, "PingAndWarm", func(ctx context.Context, headerMD, trailerMD *metadata.MD, _ gax.CallSettings) error {
		var err error
		_, err = c.client.PingAndWarm(ctx, req, grpc.Header(headerMD), grpc.Trailer(trailerMD))
		return err
	})

	return err

}

func (c *Client) newBuiltinMetricsTracer(ctx context.Context, table string, isStreaming bool) *builtinMetricsTracer {
	mt := c.metricsTracerFactory.createBuiltinMetricsTracer(ctx, table, isStreaming)
	return &mt
}
