package logqltest

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/metrics"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	v2 "github.com/grafana/loki/v3/pkg/lokifrontend/frontend/v2"
	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend/v2/frontendv2pb"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querier/worker"
	"github.com/grafana/loki/v3/pkg/scheduler"
	"github.com/grafana/loki/v3/pkg/scheduler/schedulerpb"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

// queryFrontendExecutionStack runs queries through a query-frontend + query-scheduler +
// querier-worker loop over gRPC.
type queryFrontendExecutionStack struct {
	t                    *testing.T
	stackName            string
	queryShardingEnabled bool
	handler              http.Handler

	// storeMu guards the swap against reads from in-flight sharded subqueries.
	storeMu sync.RWMutex
	store   *testingChunkStore
}

// newQueryFrontendStack builds a self-contained query-frontend + query-scheduler +
// querier-worker loop wired over gRPC.
func newQueryFrontendStack(t *testing.T, queryShardingEnabled bool) (*queryFrontendExecutionStack, error) {
	var (
		logger       = log.NewNopLogger()
		ctx          = context.Background()
		schemaConfig = newQueryFrontendSchemaConfig()
		stackName    string
	)

	if queryShardingEnabled {
		stackName = queryFrontendShardStackName
	} else {
		stackName = queryFrontendNoShardStackName
	}

	s := &queryFrontendExecutionStack{t: t, queryShardingEnabled: queryShardingEnabled, stackName: stackName}

	var shutdown []func()
	stop := func() {
		for i := len(shutdown) - 1; i >= 0; i-- {
			shutdown[i]()
		}
	}
	abort := func(err error) (*queryFrontendExecutionStack, error) {
		stop()
		return nil, err
	}

	overrides, err := newQueryFrontendOverrides()
	if err != nil {
		return abort(err)
	}

	// Start the query-scheduler.
	schedulerReg := prometheus.NewRegistry()
	var schedulerCfg scheduler.Config
	flagext.DefaultValues(&schedulerCfg)
	schedulerCfg.UseSchedulerRing = false
	schedulerInstance, err := scheduler.NewScheduler(schedulerCfg, overrides, logger, nil, schedulerReg, constants.Loki)
	if err != nil {
		return abort(err)
	}
	schedulerListener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return abort(err)
	}
	schedulerServer := grpc.NewServer()
	schedulerpb.RegisterSchedulerForFrontendServer(schedulerServer, schedulerInstance)
	schedulerpb.RegisterSchedulerForQuerierServer(schedulerServer, schedulerInstance)
	go schedulerServer.Serve(schedulerListener) //nolint:errcheck
	shutdown = append(shutdown, schedulerServer.GracefulStop)
	if err := services.StartAndAwaitRunning(ctx, schedulerInstance); err != nil {
		return abort(err)
	}
	shutdown = append(shutdown, func() { _ = services.StopAndAwaitTerminated(ctx, schedulerInstance) })
	schedulerAddr := schedulerListener.Addr().String()

	// Frontend. Its own gRPC listener receives results straight from the querier, so the
	// advertised address/port must point at it before NewFrontend wires the scheduler workers.
	frontendListener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return abort(err)
	}
	frontendHost, frontendPortStr, err := net.SplitHostPort(frontendListener.Addr().String())
	if err != nil {
		return abort(err)
	}
	frontendPort, err := strconv.Atoi(frontendPortStr)
	if err != nil {
		return abort(err)
	}
	var frontendCfg v2.Config
	flagext.DefaultValues(&frontendCfg)
	frontendCfg.SchedulerAddress = schedulerAddr
	frontendCfg.Addr = frontendHost
	frontendCfg.Port = frontendPort
	// Send the parsed query plan to the querier, not just the query string. Sharding rewrites some
	// aggregations into internal ops (e.g. __quantile_sketch_over_time__) that are not valid LogQL
	// text; carrying the plan lets the querier use the AST instead of re-parsing it.
	frontendCfg.Encoding = v2.EncodingProtobuf
	frontendInstance, err := v2.NewFrontend(frontendCfg, nil, logger, prometheus.NewRegistry(), queryrange.DefaultCodec, constants.Loki)
	if err != nil {
		return abort(err)
	}
	// The querier sends results back to the frontend over this listener with the org in gRPC
	// metadata; extract it into the context so the frontend's QueryResult can read the tenant.
	// The interceptor tolerates a missing org so the pool's health checks (no org) still pass.
	frontendGRPC := grpc.NewServer(grpc.ChainUnaryInterceptor(extractOrgUnaryInterceptor))
	frontendv2pb.RegisterFrontendForQuerierServer(frontendGRPC, frontendInstance)
	go frontendGRPC.Serve(frontendListener) //nolint:errcheck
	shutdown = append(shutdown, frontendGRPC.GracefulStop)
	if err := services.StartAndAwaitRunning(ctx, frontendInstance); err != nil {
		return abort(err)
	}
	shutdown = append(shutdown, func() { _ = services.StopAndAwaitTerminated(ctx, frontendInstance) })

	// Querier worker + handler.
	var workerCfg worker.Config
	flagext.DefaultValues(&workerCfg)
	workerCfg.SchedulerAddress = schedulerAddr
	workerCfg.QuerierID = "logqltest"
	workerCfg.MaxConcurrent = 16
	workerInstance, err := worker.NewQuerierWorker(workerCfg, nil, s.queryHandler(logger), logger, prometheus.NewRegistry(), queryrange.DefaultCodec)
	if err != nil {
		return abort(err)
	}
	if err := services.StartAndAwaitRunning(ctx, workerInstance); err != nil {
		return abort(err)
	}
	shutdown = append(shutdown, func() { _ = services.StopAndAwaitTerminated(ctx, workerInstance) })

	// Tripperware + serialize handler: decode an HTTP request, run it through the tripperware onto
	// the frontend, encode the response back.
	tripperware, stopper, err := newQueryFrontendTripperware(logger, overrides, schemaConfig, queryShardingEnabled)
	if err != nil {
		return abort(err)
	}
	if stopper != nil {
		shutdown = append(shutdown, stopper.Stop)
	}
	s.handler = queryrange.NewSerializeHTTPHandler(tripperware.Wrap(frontendInstance), queryrange.DefaultCodec)

	// Wait for the query-frontend<->query-scheduler and querier<->query-scheduler connections to establish.
	if err := waitReady(ctx, frontendInstance, schedulerReg, 15*time.Second); err != nil {
		return abort(err)
	}

	t.Cleanup(stop)
	return s, nil
}

func (s *queryFrontendExecutionStack) name() string {
	return s.stackName
}

func (s *queryFrontendExecutionStack) isQueryShardingSupported() bool {
	return s.queryShardingEnabled
}

func (*queryFrontendExecutionStack) isEvalSupported(_ evalCmd, exp expectations) bool {
	// A scalar cannot round-trip the response codec.
	return exp.scalar == nil
}

func (s *queryFrontendExecutionStack) setStreams(streams []logproto.Stream) {
	store := newScriptStore(s.t, streams)
	s.storeMu.Lock()
	old := s.store
	s.store = store
	s.storeMu.Unlock()

	// Stop the previous store so a multi-scenario script does not leave one running per refresh.
	// No query runs between evals, so the old store is idle here.
	if old != nil {
		old.close()
	}
}

func (s *queryFrontendExecutionStack) querier() logql.Querier {
	s.storeMu.RLock()
	defer s.storeMu.RUnlock()
	if s.store == nil {
		return nil
	}
	return s.store.querier()
}

func (s *queryFrontendExecutionStack) eval(cmd evalCmd) (logqlmodel.Result, error) {
	var lokiReq queryrangebase.Request
	if cmd.mode == evalInstant {
		lokiReq = &queryrange.LokiInstantRequest{
			Query:     cmd.query,
			Limit:     1000,
			TimeTs:    epoch.Add(cmd.ts),
			Direction: cmd.direction,
			Path:      "/loki/api/v1/query",
		}
	} else {
		lokiReq = &queryrange.LokiRequest{
			Query:     cmd.query,
			Limit:     1000,
			Step:      cmd.step.Milliseconds(),
			StartTs:   epoch.Add(cmd.start),
			EndTs:     epoch.Add(cmd.end),
			Direction: cmd.direction,
			Path:      "/loki/api/v1/query_range",
		}
	}

	baseCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	userCtx := user.InjectOrgID(baseCtx, tenant)

	// EncodeRequest only serialises the query string and range into a URL; it never parses, so
	// even a malformed query reaches the server, where DecodeRequest parses it and surfaces the
	// error.
	httpReq, err := queryrange.DefaultCodec.EncodeRequest(userCtx, lokiReq)
	if err != nil {
		return logqlmodel.Result{}, err
	}
	httpReq = httpReq.WithContext(userCtx)

	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, httpReq)

	respObj, err := queryrange.DefaultCodec.DecodeResponse(userCtx, rec.Result(), lokiReq)
	if err != nil {
		return logqlmodel.Result{}, err
	}
	return queryrange.ResponseToResult(respObj)
}

// queryHandler creates and returns a query handler function.
func (s *queryFrontendExecutionStack) queryHandler(logger log.Logger) queryrangebase.HandlerFunc {
	return func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		if _, ok := req.(*logproto.IndexStatsRequest); ok {
			return &queryrange.IndexStatsResponse{
				// Answer index-stats probes with a synthetic byte count that – in conjunction with
				// the configured TSDBMaxBytesPerShard – is designed to always trigger query sharding.
				Response: &logproto.IndexStatsResponse{Bytes: 4 * 1024},
			}, nil
		}

		q := s.querier()
		if q == nil {
			return nil, fmt.Errorf("store not ready")
		}

		var opts logql.EngineOpts
		flagext.DefaultValues(&opts)
		engine := logql.NewEngine(opts, q, logql.NoLimits, logger)

		params, err := queryrange.ParamsFromRequest(req)
		if err != nil {
			return nil, err
		}

		// Chunks are written under the package tenant, so run the query as that tenant.
		execCtx := user.InjectOrgID(ctx, tenant)
		res, err := engine.Query(params).Exec(execCtx)
		if err != nil {
			return nil, err
		}
		return queryrange.ResultToResponse(res, params)
	}
}

func newQueryFrontendOverrides() (*validation.Overrides, error) {
	var limits validation.Limits
	flagext.DefaultValues(&limits)

	// Set TSDBMaxBytesPerShard to a low value to deterministically fan out each query into a handful of shards.
	if err := limits.TSDBMaxBytesPerShard.Set("1024"); err != nil {
		return nil, err
	}

	// Disable split-by-interval. The metric query splitter aligns sub-query boundaries to the
	// absolute step grid, which shifts range-query points, and then causes mismatches with assertions.
	_ = limits.QuerySplitDuration.Set("0")
	_ = limits.InstantMetricQuerySplitDuration.Set("0")

	return validation.NewOverrides(limits, nil)
}

// newQueryFrontendSchemaConfig creates the schema config used by the query-frontend execution stack.
func newQueryFrontendSchemaConfig() config.SchemaConfig {
	schema := config.SchemaConfig{Configs: []config.PeriodConfig{{
		From:       config.DayTime{Time: model.Earliest},
		IndexType:  "tsdb",
		ObjectType: "filesystem",
		Schema:     "v13",
		IndexTables: config.IndexPeriodicTableConfig{
			PathPrefix:          "index/",
			PeriodicTableConfig: config.PeriodicTableConfig{Prefix: "index_", Period: 24 * time.Hour},
		},
	}}}

	// Pre-warm VersionAsInt to avoid a data race condition.
	for i := range schema.Configs {
		_, _ = schema.Configs[i].VersionAsInt()
	}

	return schema
}

func newQueryFrontendTripperware(logger log.Logger, overrides *validation.Overrides, schema config.SchemaConfig, sharded bool) (queryrangebase.Middleware, queryrange.Stopper, error) {
	var cfg queryrange.Config
	flagext.DefaultValues(&cfg)
	cfg.CacheResults = false
	cfg.CacheIndexStatsResults = false
	cfg.CacheVolumeResults = false
	cfg.CacheInstantMetricResults = false
	cfg.CacheSeriesResults = false
	cfg.CacheLabelResults = false

	// Match the direct path exactly: no step alignment, single execution (no retries).
	cfg.AlignQueriesWithStep = false
	cfg.MaxRetries = 0
	cfg.ShardedQueries = sharded

	// Enable the aggregations that only shard behind this flag. quantile_over_time uses the
	// count-min/quantile sketch path; first/last_over_time use the timestamp-carrying merge path.
	cfg.ShardAggregations = []string{"quantile_over_time", "first_over_time", "last_over_time", "approx_topk"}

	var engineOpts logql.EngineOpts
	flagext.DefaultValues(&engineOpts)

	return queryrange.NewMiddleware(
		cfg,
		engineOpts,
		queryrange.RouterConfig{},
		nil,
		logger,
		overrides,
		schema,
		nil,
		false,
		prometheus.NewRegistry(),
		constants.Loki,
	)
}

func waitReady(ctx context.Context, frontendInstance *v2.Frontend, schedulerReg prometheus.Gatherer, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var connectedQueriers float64
	for time.Now().Before(deadline) {
		mfm, err := metrics.NewMetricFamilyMapFromGatherer(schedulerReg)
		if err != nil {
			return err
		}
		connectedQueriers = mfm.SumGauges("loki_query_scheduler_connected_querier_clients")
		if frontendInstance.CheckReady(ctx) == nil && connectedQueriers >= 1 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("frontend/scheduler/querier connections did not establish within %s (frontend ready=%v, connected queriers=%v)",
		timeout, frontendInstance.CheckReady(ctx) == nil, connectedQueriers)
}

// extractOrgUnaryInterceptor lifts the org id from gRPC metadata into the request context so the
// frontend's QueryResult can read the tenant. A missing org is tolerated so an unauthenticated
// call still passes.
func extractOrgUnaryInterceptor(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	if _, newCtx, err := user.ExtractFromGRPCRequest(ctx); err == nil {
		ctx = newCtx
	}
	return handler(ctx, req)
}
