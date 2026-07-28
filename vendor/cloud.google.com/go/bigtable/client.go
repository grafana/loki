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
	disableRetryInfo        bool
	retryOption             gax.CallOption
	executeQueryRetryOption gax.CallOption
	featureFlagsMD          metadata.MD // Pre-computed feature flags metadata to be sent with each request.
	mPool                   btransport.ManagedChannelPool
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
	metricsTracerFactory, err := metrics.NewFactory(ctx, project, instance, config.AppProfile, metricsProvider, opts...)
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

	var directAccessOptions = []option.ClientOption{
		internaloption.EnableDirectPath(true),
		internaloption.EnableDirectPathXds(),
		internaloption.AllowHardBoundTokens("ALTS"),
	}

	// Allow non-default service account in DirectPath.
	o = append(o, internaloption.AllowNonDefaultServiceAccount(true))
	o = append(o, opts...)
	o = append(o, internaloption.EnableNewAuthLibrary())
	o = append(o, internaloption.EnableJwtWithScope())

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

	allowDirectAccess := isDirectAccessEnabled(config)
	directAccessMD := createFeatureFlagsMD(metricsTracerFactory.Enabled, disableRetryInfo, allowDirectAccess)

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

	return &Client{
		connPool:                mPool.Pool,
		client:                  btpb.NewBigtableClient(mPool.Pool),
		project:                 project,
		instance:                instance,
		appProfile:              config.AppProfile,
		metricsTracerFactory:    metricsTracerFactory,
		disableRetryInfo:        disableRetryInfo,
		retryOption:             retryOption,
		executeQueryRetryOption: executeQueryRetryOption,
		featureFlagsMD:          directAccessMD,
		mPool:                   mPool,
	}, nil
}

// Close closes the Client.
func (c *Client) Close() error {
	if c.metricsTracerFactory != nil {
		c.metricsTracerFactory.Shutdown()
	}
	return c.mPool.Close()
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

func isDirectAccessEnabled(config ClientConfig) bool {
	if os.Getenv(directAccessEnvVar) == "" {
		return !config.DisableDirectAccess
	}
	res, _ := strconv.ParseBool(os.Getenv(directAccessEnvVar))
	return res
}
