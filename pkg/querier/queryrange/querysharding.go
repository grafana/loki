package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/marshal"
	"github.com/grafana/loki/pkg/util/spanlogger"
	"github.com/grafana/loki/pkg/util/validation"
)

var errInvalidShardingRange = errors.New("Query does not fit in a single sharding configuration")

// NewQueryShardMiddleware creates a middleware which downstreams queries after AST mapping and query encoding.
func NewQueryShardMiddleware(
	logger log.Logger,
	confs ShardingConfigs,
	engineOpts logql.EngineOpts,
	codec queryrangebase.Codec,
	middlewareMetrics *queryrangebase.InstrumentMiddlewareMetrics,
	shardingMetrics *logql.MapperMetrics,
	limits Limits,
	maxShards int,
	statsHandler queryrangebase.Handler,
) queryrangebase.Middleware {
	noshards := !hasShards(confs)

	if noshards {
		level.Warn(logger).Log(
			"middleware", "QueryShard",
			"msg", "no configuration with shard found",
			"confs", fmt.Sprintf("%+v", confs),
		)
		return queryrangebase.PassthroughMiddleware
	}

	mapperware := queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return newASTMapperware(confs, engineOpts, next, statsHandler, logger, shardingMetrics, limits, maxShards)
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
	confs ShardingConfigs,
	engineOpts logql.EngineOpts,
	next queryrangebase.Handler,
	statsHandler queryrangebase.Handler,
	logger log.Logger,
	metrics *logql.MapperMetrics,
	limits Limits,
	maxShards int,
) *astMapperware {
	ast := &astMapperware{
		confs:        confs,
		logger:       log.With(logger, "middleware", "QueryShard.astMapperware"),
		limits:       limits,
		next:         next,
		statsHandler: next,
		ng:           logql.NewDownstreamEngine(engineOpts, DownstreamHandler{next: next, limits: limits}, limits, logger),
		metrics:      metrics,
		maxShards:    maxShards,
	}

	if statsHandler != nil {
		ast.statsHandler = statsHandler
	}

	return ast
}

type astMapperware struct {
	confs        ShardingConfigs
	logger       log.Logger
	limits       Limits
	next         queryrangebase.Handler
	statsHandler queryrangebase.Handler
	ng           *logql.DownstreamEngine
	metrics      *logql.MapperMetrics
	maxShards    int
}

func (ast *astMapperware) checkQuerySizeLimit(ctx context.Context, bytesPerShard uint64, notShardable bool) error {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	maxQuerierBytesReadCapture := func(id string) int { return ast.limits.MaxQuerierBytesRead(ctx, id) }
	if maxBytesRead := validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, maxQuerierBytesReadCapture); maxBytesRead > 0 {
		statsBytesStr := humanize.IBytes(bytesPerShard)
		maxBytesReadStr := humanize.IBytes(uint64(maxBytesRead))

		if bytesPerShard > uint64(maxBytesRead) {
			level.Warn(ast.logger).Log("msg", "Query exceeds limits", "status", "rejected", "limit_name", "MaxQuerierBytesRead", "limit_bytes", maxBytesReadStr, "resolved_bytes", statsBytesStr)

			errorTmpl := limErrQuerierTooManyBytesShardableTmpl
			if notShardable {
				errorTmpl = limErrQuerierTooManyBytesUnshardableTmpl
			}

			return httpgrpc.Errorf(http.StatusBadRequest, errorTmpl, statsBytesStr, maxBytesReadStr)
		}

		level.Debug(ast.logger).Log("msg", "Query is within limits", "status", "accepted", "limit_name", "MaxQuerierBytesRead", "limit_bytes", maxBytesReadStr, "resolved_bytes", statsBytesStr)
	}

	return nil
}

func (ast *astMapperware) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	logger := spanlogger.FromContextWithFallback(
		ctx,
		util_log.WithContext(ctx, ast.logger),
	)

	maxRVDuration, maxOffset, err := maxRangeVectorAndOffsetDuration(r.GetQuery())
	if err != nil {
		level.Warn(logger).Log("err", err.Error(), "msg", "failed to get range-vector and offset duration so skipped AST mapper for request")
		return ast.next.Do(ctx, r)
	}

	conf, err := ast.confs.GetConf(int64(model.Time(r.GetStart()).Add(-maxRVDuration).Add(-maxOffset)), int64(model.Time(r.GetEnd()).Add(-maxOffset)))
	// cannot shard with this timerange
	if err != nil {
		level.Warn(logger).Log("err", err.Error(), "msg", "skipped AST mapper for request")
		return ast.next.Do(ctx, r)
	}

	tenants, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	resolver, ok := shardResolverForConf(
		ctx,
		conf,
		ast.ng.Opts().MaxLookBackPeriod,
		ast.logger,
		MinWeightedParallelism(ctx, tenants, ast.confs, ast.limits, model.Time(r.GetStart()), model.Time(r.GetEnd())),
		ast.maxShards,
		r,
		ast.statsHandler,
		ast.limits,
	)
	if !ok {
		return ast.next.Do(ctx, r)
	}

	mapper := logql.NewShardMapper(resolver, ast.metrics)

	noop, bytesPerShard, parsed, err := mapper.Parse(r.GetQuery())
	if err != nil {
		level.Warn(logger).Log("msg", "failed mapping AST", "err", err.Error(), "query", r.GetQuery())
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

	params, err := paramsFromRequest(r)
	if err != nil {
		return nil, err
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
	query := ast.ng.Query(ctx, params, parsed)

	res, err := query.Exec(ctx)
	if err != nil {
		return nil, err
	}

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
				Headers: res.Headers,
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
			Headers: respHeaders,
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
				Headers: res.Headers,
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
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}
	minShardingLookback := validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, splitter.limits.MinShardingLookback)
	if minShardingLookback == 0 {
		return splitter.shardingware.Do(ctx, r)
	}
	cutoff := splitter.now().Add(-minShardingLookback)
	// Only attempt to shard queries which are older than the sharding lookback
	// (the period for which ingesters are also queried) or when the lookback is disabled.
	if minShardingLookback == 0 || util.TimeFromMillis(r.GetEnd()).Before(cutoff) {
		return splitter.shardingware.Do(ctx, r)
	}
	return splitter.next.Do(ctx, r)
}

func hasShards(confs ShardingConfigs) bool {
	for _, conf := range confs {
		if conf.RowShards > 0 || conf.IndexType == config.TSDBType {
			return true
		}
	}
	return false
}

// ShardingConfigs is a slice of chunk shard configs
type ShardingConfigs []config.PeriodConfig

// ValidRange extracts a non-overlapping sharding configuration from a list of configs and a time range.
func (confs ShardingConfigs) ValidRange(start, end int64) (config.PeriodConfig, error) {
	for i, conf := range confs {
		if start < int64(conf.From.Time) {
			// the query starts before this config's range
			return config.PeriodConfig{}, errInvalidShardingRange
		} else if i == len(confs)-1 {
			// the last configuration has no upper bound
			return conf, nil
		} else if end < int64(confs[i+1].From.Time) {
			// The request is entirely scoped into this shard config
			return conf, nil
		} else {
			continue
		}
	}

	return config.PeriodConfig{}, errInvalidShardingRange
}

// GetConf will extract a shardable config corresponding to a request and the shardingconfigs
func (confs ShardingConfigs) GetConf(start, end int64) (config.PeriodConfig, error) {
	conf, err := confs.ValidRange(start, end)
	// query exists across multiple sharding configs
	if err != nil {
		return conf, err
	}

	// query doesn't have shard factor, so don't try to do AST mapping.
	if conf.RowShards < 2 && conf.IndexType != config.TSDBType {
		return conf, errors.Errorf("shard factor not high enough: [%d]", conf.RowShards)
	}

	return conf, nil
}

// NewSeriesQueryShardMiddleware creates a middleware which shards series queries.
func NewSeriesQueryShardMiddleware(
	logger log.Logger,
	confs ShardingConfigs,
	middlewareMetrics *queryrangebase.InstrumentMiddlewareMetrics,
	shardingMetrics *logql.MapperMetrics,
	limits Limits,
	merger queryrangebase.Merger,
) queryrangebase.Middleware {
	noshards := !hasShards(confs)

	if noshards {
		level.Warn(logger).Log(
			"middleware", "QueryShard",
			"msg", "no configuration with shard found",
			"confs", fmt.Sprintf("%+v", confs),
		)
		return queryrangebase.PassthroughMiddleware
	}
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
	confs   ShardingConfigs
	logger  log.Logger
	next    queryrangebase.Handler
	metrics *logql.MapperMetrics
	limits  Limits
	merger  queryrangebase.Merger
}

func (ss *seriesShardingHandler) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	conf, err := ss.confs.GetConf(r.GetStart(), r.GetEnd())
	// cannot shard with this timerange
	if err != nil {
		level.Warn(ss.logger).Log("err", err.Error(), "msg", "skipped sharding for request")
		return ss.next.Do(ctx, r)
	}

	req, ok := r.(*LokiSeriesRequest)
	if !ok {
		return nil, fmt.Errorf("expected *LokiSeriesRequest, got (%T)", r)
	}

	ss.metrics.DownstreamQueries.WithLabelValues("series").Inc()
	ss.metrics.DownstreamFactor.Observe(float64(conf.RowShards))

	requests := make([]queryrangebase.Request, 0, conf.RowShards)
	for i := 0; i < int(conf.RowShards); i++ {
		shardedRequest := *req
		shardedRequest.Shards = []string{astmapper.ShardAnnotation{
			Shard: i,
			Of:    int(conf.RowShards),
		}.String()}
		requests = append(requests, &shardedRequest)
	}

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}
	requestResponses, err := queryrangebase.DoRequests(
		ctx,
		ss.next,
		requests,
		MinWeightedParallelism(ctx, tenantIDs, ss.confs, ss.limits, model.Time(req.GetStart()), model.Time(req.GetEnd())),
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
