package queryrange

import (
	"context"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/util/marshal"
)

// NewQueryShardMiddleware creates a middleware which downstreams queries after AST mapping and query encoding.
func NewQueryShardMiddleware(
	logger log.Logger,
	confs queryrange.ShardingConfigs,
	middlewareMetrics *queryrange.InstrumentMiddlewareMetrics,
	shardingMetrics *logql.ShardingMetrics,
	limits logql.Limits,
) queryrange.Middleware {

	noshards := !hasShards(confs)

	if noshards {
		level.Warn(logger).Log(
			"middleware", "QueryShard",
			"msg", "no configuration with shard found",
			"confs", fmt.Sprintf("%+v", confs),
		)
		return queryrange.PassthroughMiddleware
	}

	mapperware := queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
		return newASTMapperware(confs, next, logger, shardingMetrics, limits)
	})

	return queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
		return queryrange.MergeMiddlewares(
			queryrange.InstrumentMiddleware("shardingware", middlewareMetrics),
			mapperware,
		).Wrap(next)
	})
}

func newASTMapperware(
	confs queryrange.ShardingConfigs,
	next queryrange.Handler,
	logger log.Logger,
	metrics *logql.ShardingMetrics,
	limits logql.Limits,
) *astMapperware {

	return &astMapperware{
		confs:   confs,
		logger:  log.With(logger, "middleware", "QueryShard.astMapperware"),
		next:    next,
		ng:      logql.NewShardedEngine(logql.EngineOpts{}, DownstreamHandler{next}, metrics, limits),
		metrics: metrics,
	}
}

type astMapperware struct {
	confs   queryrange.ShardingConfigs
	logger  log.Logger
	next    queryrange.Handler
	ng      *logql.ShardedEngine
	metrics *logql.ShardingMetrics
}

func (ast *astMapperware) Do(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
	conf, err := ast.confs.GetConf(r)
	// cannot shard with this timerange
	if err != nil {
		level.Warn(ast.logger).Log("err", err.Error(), "msg", "skipped AST mapper for request")
		return ast.next.Do(ctx, r)
	}

	shardedLog, ctx := spanlogger.New(ctx, "shardedEngine")
	defer shardedLog.Finish()

	mapper, err := logql.NewShardMapper(int(conf.RowShards), ast.metrics)
	if err != nil {
		return nil, err
	}

	noop, parsed, err := mapper.Parse(r.GetQuery())
	if err != nil {
		level.Warn(shardedLog).Log("msg", "failed mapping AST", "err", err.Error(), "query", r.GetQuery())
		return nil, err
	}
	level.Debug(shardedLog).Log("no-op", noop, "mapped", parsed.String())

	if noop {
		// the ast can't be mapped to a sharded equivalent
		// so we can bypass the sharding engine.
		return ast.next.Do(ctx, r)
	}

	var params logql.Params
	var path string
	switch r := r.(type) {
	case *LokiRequest:
		params = paramsFromRequest(r)
		path = r.GetPath()
	case *LokiInstantRequest:
		params = paramsFromInstantRequest(r)
		path = r.GetPath()
	default:
		return nil, fmt.Errorf("expected *LokiRequest or *LokiInstantRequest, got (%T)", r)
	}
	query := ast.ng.Query(params, parsed)

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
			Response: &queryrange.PrometheusResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: queryrange.PrometheusData{
					ResultType: loghttp.ResultTypeMatrix,
					Result:     toProtoMatrix(value.(loghttp.Matrix)),
				},
			},
			Statistics: res.Statistics,
		}, nil
	case logqlmodel.ValueTypeStreams:
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
		}, nil
	case parser.ValueTypeVector:
		return &LokiPromResponse{Response: &queryrange.PrometheusResponse{
			Status: loghttp.QueryStatusSuccess,
			Data: queryrange.PrometheusData{
				ResultType: loghttp.ResultTypeVector,
				Result:     toProtoVector(value.(loghttp.Vector)),
			},
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
	MinShardingLookback time.Duration      // delimiter for splitting sharded vs non-sharded queries
	shardingware        queryrange.Handler // handler for sharded queries
	next                queryrange.Handler // handler for non-sharded queries
	now                 func() time.Time   // injectable time.Now
}

func (splitter *shardSplitter) Do(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
	cutoff := splitter.now().Add(-splitter.MinShardingLookback)

	// Only attempt to shard queries which are older than the sharding lookback (the period for which ingesters are also queried).
	if !cutoff.After(util.TimeFromMillis(r.GetEnd())) {
		return splitter.next.Do(ctx, r)
	}
	return splitter.shardingware.Do(ctx, r)
}

// TODO(owen-d): export in cortex so we don't duplicate code
func hasShards(confs queryrange.ShardingConfigs) bool {
	for _, conf := range confs {
		if conf.RowShards > 0 {
			return true
		}
	}
	return false
}

// NewSeriesQueryShardMiddleware creates a middleware which shards series queries.
func NewSeriesQueryShardMiddleware(
	logger log.Logger,
	confs queryrange.ShardingConfigs,
	middlewareMetrics *queryrange.InstrumentMiddlewareMetrics,
	shardingMetrics *logql.ShardingMetrics,
	limits queryrange.Limits,
	merger queryrange.Merger,
) queryrange.Middleware {

	noshards := !hasShards(confs)

	if noshards {
		level.Warn(logger).Log(
			"middleware", "QueryShard",
			"msg", "no configuration with shard found",
			"confs", fmt.Sprintf("%+v", confs),
		)
		return queryrange.PassthroughMiddleware
	}
	return queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
		return queryrange.InstrumentMiddleware("sharding", middlewareMetrics).Wrap(
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
	confs   queryrange.ShardingConfigs
	logger  log.Logger
	next    queryrange.Handler
	metrics *logql.ShardingMetrics
	limits  queryrange.Limits
	merger  queryrange.Merger
}

func (ss *seriesShardingHandler) Do(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
	conf, err := ss.confs.GetConf(r)
	// cannot shard with this timerange
	if err != nil {
		level.Warn(ss.logger).Log("err", err.Error(), "msg", "skipped sharding for request")
		return ss.next.Do(ctx, r)
	}

	if conf.RowShards <= 1 {
		return ss.next.Do(ctx, r)
	}

	req, ok := r.(*LokiSeriesRequest)
	if !ok {
		return nil, fmt.Errorf("expected *LokiSeriesRequest, got (%T)", r)
	}

	ss.metrics.Shards.WithLabelValues("series").Inc()
	ss.metrics.ShardFactor.Observe(float64(conf.RowShards))

	requests := make([]queryrange.Request, 0, conf.RowShards)
	for i := 0; i < int(conf.RowShards); i++ {
		shardedRequest := *req
		shardedRequest.Shards = []string{astmapper.ShardAnnotation{
			Shard: i,
			Of:    int(conf.RowShards),
		}.String()}
		requests = append(requests, &shardedRequest)
	}
	requestResponses, err := queryrange.DoRequests(ctx, ss.next, requests, ss.limits)
	if err != nil {
		return nil, err
	}
	responses := make([]queryrange.Response, 0, len(requestResponses))
	for _, res := range requestResponses {
		responses = append(responses, res.Response)
	}
	return ss.merger.MergeResponse(responses...)
}
