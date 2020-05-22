package queryrange

import (
	"context"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/marshal"
)

var nanosecondsInMillisecond = int64(time.Millisecond / time.Nanosecond)

// NewQueryShardMiddleware creates a middleware which downstreams queries after AST mapping and query encoding.
func NewQueryShardMiddleware(
	logger log.Logger,
	confs queryrange.ShardingConfigs,
	minShardingLookback time.Duration,
	middlewareMetrics *queryrange.InstrumentMiddlewareMetrics,
	shardingMetrics *logql.ShardingMetrics,
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
		return newASTMapperware(confs, next, logger, shardingMetrics)
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
) *astMapperware {

	return &astMapperware{
		confs:  confs,
		logger: log.With(logger, "middleware", "QueryShard.astMapperware"),
		next:   next,
		ng:     logql.NewShardedEngine(logql.EngineOpts{}, DownstreamHandler{next}, metrics),
	}
}

type astMapperware struct {
	confs  queryrange.ShardingConfigs
	logger log.Logger
	next   queryrange.Handler
	ng     *logql.ShardedEngine
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
	params := paramsFromRequest(req)
	query := ast.ng.Query(params, int(conf.RowShards))

	res, err := query.Exec(ctx)
	if err != nil {
		return nil, err
	}

	value, err := marshal.NewResultValue(res.Data)
	if err != nil {
		return nil, err
	}

	switch res.Data.Type() {
	case promql.ValueTypeMatrix:
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
	sharded, nonsharded := partitionRequest(r, cutoff)

	return splitter.parallel(ctx, sharded, nonsharded)

}

func (splitter *shardSplitter) parallel(ctx context.Context, sharded, nonsharded queryrange.Request) (queryrange.Response, error) {
	if sharded == nil {
		return splitter.next.Do(ctx, nonsharded)
	}

	if nonsharded == nil {
		return splitter.shardingware.Do(ctx, sharded)
	}

	nonshardCh := make(chan queryrange.Response, 1)
	shardCh := make(chan queryrange.Response, 1)
	errCh := make(chan error, 2)

	go func() {
		res, err := splitter.next.Do(ctx, nonsharded)
		if err != nil {
			errCh <- err
			return
		}
		nonshardCh <- res

	}()

	go func() {
		res, err := splitter.shardingware.Do(ctx, sharded)
		if err != nil {
			errCh <- err
			return
		}
		shardCh <- res
	}()

	resps := make([]queryrange.Response, 0, 2)
	for i := 0; i < 2; i++ {
		select {
		case r := <-nonshardCh:
			resps = append(resps, r)
		case r := <-shardCh:
			resps = append(resps, r)
		case err := <-errCh:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}

	}

	return lokiCodec.MergeResponse(resps...)
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

// partitionRequet splits a request into potentially multiple requests, one including the request's time range
// [0,t). The other will include [t,inf)
// TODO(owen-d): export in cortex so we don't duplicate code
func partitionRequest(r queryrange.Request, t time.Time) (before queryrange.Request, after queryrange.Request) {
	boundary := TimeToMillis(t)
	if r.GetStart() >= boundary {
		return nil, r
	}

	if r.GetEnd() < boundary {
		return r, nil
	}

	return r.WithStartEnd(r.GetStart(), boundary), r.WithStartEnd(boundary, r.GetEnd())
}

// TimeFromMillis is a helper to turn milliseconds -> time.Time
func TimeFromMillis(ms int64) time.Time {
	return time.Unix(0, ms*nanosecondsInMillisecond)
}

func TimeToMillis(t time.Time) int64 {
	return t.UnixNano() / nanosecondsInMillisecond
}
