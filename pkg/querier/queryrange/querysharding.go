package queryrange

import (
	"context"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/marshal"
)

// NewQueryShardMiddleware creates a middleware which downstreams queries after AST mapping and query encoding.
func NewQueryShardMiddleware(
	logger log.Logger,
	confs queryrange.ShardingConfigs,
	minShardingLookback time.Duration,
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
		return &shardSplitter{
			MinShardingLookback: minShardingLookback,
			shardingware: queryrange.MergeMiddlewares(
				queryrange.InstrumentMiddleware("shardingware", middlewareMetrics),
				mapperware,
			).Wrap(next),
			now:  time.Now,
			next: queryrange.InstrumentMiddleware("sharding-bypass", middlewareMetrics).Wrap(next),
		}
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

	req, ok := r.(*LokiRequest)
	if !ok {
		return nil, fmt.Errorf("expected *LokiRequest, got (%T)", r)
	}

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

	query := ast.ng.Query(paramsFromRequest(req), parsed)

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
					Result:     toProto(value.(loghttp.Matrix)),
				},
			},
			Statistics: res.Statistics,
		}, nil
	case logql.ValueTypeStreams:
		return &LokiResponse{
			Status:     loghttp.QueryStatusSuccess,
			Direction:  req.Direction,
			Limit:      req.Limit,
			Version:    uint32(loghttp.GetVersion(req.Path)),
			Statistics: res.Statistics,
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result:     value.(loghttp.Streams).ToProto(),
			},
		}, nil
	default:
		return nil, fmt.Errorf("unexpected downstream response type (%T)", res.Data)
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
