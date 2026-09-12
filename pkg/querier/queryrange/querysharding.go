package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/marshal"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

// NewQueryShardMiddleware creates a middleware which downstreams queries after AST mapping and query encoding.
func NewQueryShardMiddleware(
	logger log.Logger,
	confs []config.PeriodConfig,
	engineOpts logql.EngineOpts,
	middlewareMetrics *queryrangebase.InstrumentMiddlewareMetrics,
	shardingMetrics *logql.MapperMetrics,
	limits Limits,
	maxShards int,
	statsHandler queryrangebase.Handler,
	retryNextHandler queryrangebase.Handler,
	shardAggregation []string,
) queryrangebase.Middleware {
	mapperware := queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return newASTMapperware(confs, engineOpts, next, retryNextHandler, statsHandler, logger, shardingMetrics, limits, maxShards, shardAggregation)
	})

	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &shardSplitter{
			limits: limits,
			shardingware: queryrangebase.MergeMiddlewares(
				queryrangebase.InstrumentMiddleware("shardingware", middlewareMetrics),
				mapperware,
			).Wrap(next),
			now:  time.Now,
			next: queryrangebase.InstrumentMiddleware("sharding-bypass", middlewareMetrics).Wrap(next),
		}
	})
}

func newASTMapperware(
	confs []config.PeriodConfig,
	engineOpts logql.EngineOpts,
	next queryrangebase.Handler,
	retryNextHandler queryrangebase.Handler,
	statsHandler queryrangebase.Handler,
	logger log.Logger,
	metrics *logql.MapperMetrics,
	limits Limits,
	maxShards int,
	shardAggregation []string,
) *astMapperware {
	ast := &astMapperware{
		confs:            confs,
		logger:           log.With(logger, "middleware", "QueryShard.astMapperware"),
		limits:           limits,
		next:             next,
		retryNextHandler: retryNextHandler,
		statsHandler:     next,
		ng:               logql.NewDownstreamEngine(engineOpts, DownstreamHandler{next: next, limits: limits}, limits, logger),
		metrics:          metrics,
		maxShards:        maxShards,
		shardAggregation: shardAggregation,
	}

	if statsHandler != nil {
		ast.statsHandler = statsHandler
	}

	return ast
}

type astMapperware struct {
	confs            []config.PeriodConfig
	logger           log.Logger
	limits           Limits
	next             queryrangebase.Handler
	retryNextHandler queryrangebase.Handler
	statsHandler     queryrangebase.Handler
	ng               *logql.DownstreamEngine
	metrics          *logql.MapperMetrics
	maxShards        int

	// Feature flag for sharding range and vector aggregations such as
	// quantile_ver_time with probabilistic data structures.
	shardAggregation []string
}

func (ast *astMapperware) checkQuerySizeLimit(ctx context.Context, bytesPerShard uint64, notShardable bool) error {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	maxQuerierBytesReadCapture := func(id string) int { return ast.limits.MaxQuerierBytesRead(ctx, id) }
	if maxBytesRead := validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, maxQuerierBytesReadCapture); maxBytesRead > 0 {
		statsBytesStr := humanize.IBytes(bytesPerShard)
		maxBytesReadStr := humanize.IBytes(uint64(maxBytesRead))

		spec := maxQuerierBytesReadShardableSpec
		if notShardable {
			spec = maxQuerierBytesReadUnshardableSpec
		}

		if bytesPerShard > uint64(maxBytesRead) {
			level.Warn(ast.logger).Log("msg", "Query exceeds limits", "status", "rejected", "limit_name", spec.limitName, "limit_bytes", maxBytesReadStr, "resolved_bytes", statsBytesStr)

			return spec.exceededErr(statsBytesStr, maxBytesReadStr)
		}

		level.Debug(ast.logger).Log("msg", "Query is within limits", "status", "accepted", "limit_name", spec.limitName, "limit_bytes", maxBytesReadStr, "resolved_bytes", statsBytesStr)
	}

	return nil
}

func (ast *astMapperware) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	logger := util_log.WithContext(ctx, ast.logger)
	spLogger := spanlogger.FromContext(
		ctx,
		logger,
	)

	params, err := ParamsFromRequest(r)
	if err != nil {
		return nil, err
	}

	tenants, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	// The shard resolver uses index stats to determine the number of shards.
	// We want to store the cache stats for the requests to get the index stats.
	// Later on, the query engine overwrites the stats context with other stats,
	// so we create a separate stats context here for the resolver that we
	// will merge with the stats returned from the engine.
	resolverStats, ctx := stats.NewContext(ctx)

	resolver, ok := newDynamicShardResolver(
		ctx,
		ast.ng.Opts().MaxLookBackPeriod,
		ast.logger,
		MinWeightedParallelism(ctx, tenants, ast.confs, ast.limits, model.Time(r.GetStart().UnixMilli()), model.Time(r.GetEnd().UnixMilli())),
		ast.maxShards,
		r,
		ast.statsHandler,
		ast.retryNextHandler,
		ast.next,
		ast.limits,
	)
	if !ok {
		return ast.next.Do(ctx, r)
	}

	// TSDB is the only supported index type, so the resolver is always dynamic
	// and the sharding strategy is selected per-tenant.
	v := ast.limits.TSDBShardingStrategy(ctx, tenants[0])
	version, err := logql.ParseShardVersion(v)
	if err != nil {
		level.Warn(spLogger).Log(
			"msg", "failed to parse shard version",
			"fallback", version.String(),
			"err", err.Error(),
			"user", tenants[0],
			"query", r.GetQuery(),
		)
	}
	strategy := version.Strategy(resolver, uint64(ast.limits.TSDBMaxBytesPerShard(tenants[0])))

	// Merge global shard aggregations and tenant overrides.
	limitShardAggregation := validation.IntersectionPerTenant(tenants, func(tenant string) []string {
		return ast.limits.ShardAggregations(tenant)
	})
	mergedShardAggregation := slices.Compact(append(limitShardAggregation, ast.shardAggregation...))

	mapper := logql.NewShardMapper(strategy, ast.metrics, mergedShardAggregation)

	noop, bytesPerShard, parsed, err := mapper.Parse(params.GetExpression())
	if err != nil {
		level.Warn(spLogger).Log("msg", "failed mapping AST", "err", err.Error(), "query", r.GetQuery())
		return nil, err
	}
	level.Debug(logger).Log("no-op", noop, "mapped", parsed.String())

	// Note, even if noop, bytesPerShard contains the bytes that'd be read for the whole expr without sharding
	if err = ast.checkQuerySizeLimit(ctx, bytesPerShard, noop); err != nil {
		return nil, err
	}

	// If the ast can't be mapped to a sharded equivalent,
	// we can bypass the sharding engine and forward the request downstream.
	if noop {
		return ast.next.Do(ctx, r)
	}

	var path string
	switch r := r.(type) {
	case *LokiRequest:
		path = r.GetPath()
	case *LokiInstantRequest:
		path = r.GetPath()
	default:
		return nil, fmt.Errorf("expected *LokiRequest or *LokiInstantRequest, got (%T)", r)
	}
	query := ast.ng.Query(ctx, logql.ParamsWithExpressionOverride{Params: params, ExpressionOverride: parsed})

	res, err := query.Exec(ctx)
	if err != nil {
		// Keep the usage of the shards and index lookups that completed before
		// the failure.
		stats.JoinPartial(ctx, res.Statistics)
		stats.JoinPartial(ctx, resolverStats.Result(0, 0, 0))
		return nil, err
	}

	// Merge index and volume stats result cache stats from shard resolver into the query stats.
	res.Statistics.Merge(resolverStats.Result(0, 0, 0))
	value, err := marshal.NewResultValue(res.Data)
	if err != nil {
		return nil, err
	}

	switch res.Data.Type() {
	case parser.ValueTypeMatrix:
		return &LokiPromResponse{
			Response: &queryrangebase.PrometheusResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: queryrangebase.PrometheusData{
					ResultType: loghttp.ResultTypeMatrix,
					Result:     toProtoMatrix(value.(loghttp.Matrix)),
				},
				Headers:  res.Headers,
				Warnings: res.Warnings,
			},
			Statistics: res.Statistics,
		}, nil
	case logqlmodel.ValueTypeStreams:
		respHeaders := make([]queryrangebase.PrometheusResponseHeader, 0, len(res.Headers))
		for i := range res.Headers {
			respHeaders = append(respHeaders, *res.Headers[i])
		}

		return &LokiResponse{
			Status:     loghttp.QueryStatusSuccess,
			Direction:  params.Direction(),
			Limit:      params.Limit(),
			Version:    uint32(loghttp.GetVersion(path)),
			Statistics: res.Statistics,
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result:     value.(loghttp.Streams).ToProto(),
			},
			Headers:  respHeaders,
			Warnings: res.Warnings,
		}, nil
	case parser.ValueTypeVector:
		return &LokiPromResponse{
			Statistics: res.Statistics,
			Response: &queryrangebase.PrometheusResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: queryrangebase.PrometheusData{
					ResultType: loghttp.ResultTypeVector,
					Result:     toProtoVector(value.(loghttp.Vector)),
				},
				Headers:  res.Headers,
				Warnings: res.Warnings,
			},
		}, nil
	default:
		return nil, fmt.Errorf("unexpected downstream response type (%T)", res.Data.Type())
	}
}

// shardSplitter middleware will only shard appropriate requests that do not extend past the MinShardingLookback interval.
// This is used to send nonsharded requests to the ingesters in order to not overload them.
// TODO(owen-d): export in cortex so we don't duplicate code
type shardSplitter struct {
	limits       Limits                 // delimiter for splitting sharded vs non-sharded queries
	shardingware queryrangebase.Handler // handler for sharded queries
	next         queryrangebase.Handler // handler for non-sharded queries
	now          func() time.Time       // injectable time.Now
}

func (splitter *shardSplitter) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}
	minShardingLookback := validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, splitter.limits.MinShardingLookback)
	if minShardingLookback == 0 {
		return splitter.shardingware.Do(ctx, r)
	}
	cutoff := splitter.now().Add(-minShardingLookback)
	// Only attempt to shard queries which are older than the sharding lookback
	// (the period for which ingesters are also queried) or when the lookback is disabled.
	if minShardingLookback == 0 || util.TimeFromMillis(r.GetEnd().UnixMilli()).Before(cutoff) {
		return splitter.shardingware.Do(ctx, r)
	}
	return splitter.next.Do(ctx, r)
}

// NewSeriesQueryShardMiddleware creates a middleware which shards series queries.
func NewSeriesQueryShardMiddleware(
	logger log.Logger,
	confs []config.PeriodConfig,
	middlewareMetrics *queryrangebase.InstrumentMiddlewareMetrics,
	shardingMetrics *logql.MapperMetrics,
	limits Limits,
	merger queryrangebase.Merger,
) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return queryrangebase.InstrumentMiddleware("sharding", middlewareMetrics).Wrap(
			&seriesShardingHandler{
				confs:   confs,
				logger:  logger,
				next:    next,
				metrics: shardingMetrics,
				limits:  limits,
				merger:  merger,
			},
		)
	})
}

type seriesShardingHandler struct {
	confs   []config.PeriodConfig
	logger  log.Logger
	next    queryrangebase.Handler
	metrics *logql.MapperMetrics
	limits  Limits
	merger  queryrangebase.Merger
}

func (ss *seriesShardingHandler) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	req, ok := r.(*LokiSeriesRequest)
	if !ok {
		return nil, fmt.Errorf("expected *LokiSeriesRequest, got (%T)", r)
	}

	ss.metrics.DownstreamQueries.WithLabelValues("series").Inc()
	ss.metrics.DownstreamFactor.Observe(float64(config.DefaultRowShards))

	requests := make([]queryrangebase.Request, 0, config.DefaultRowShards)
	for i := 0; i < config.DefaultRowShards; i++ {
		shardedRequest := *req
		shardedRequest.Shards = []string{astmapper.ShardAnnotation{
			Shard: i,
			Of:    config.DefaultRowShards,
		}.String()}
		requests = append(requests, &shardedRequest)
	}

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}
	requestResponses, err := queryrangebase.DoRequests(
		ctx,
		ss.next,
		requests,
		MinWeightedParallelism(ctx, tenantIDs, ss.confs, ss.limits, model.Time(req.GetStart().UnixMilli()), model.Time(req.GetEnd().UnixMilli())),
	)
	if err != nil {
		return nil, err
	}
	responses := make([]queryrangebase.Response, 0, len(requestResponses))
	for _, res := range requestResponses {
		responses = append(responses, res.Response)
	}
	return ss.merger.MergeResponse(responses...)
}
