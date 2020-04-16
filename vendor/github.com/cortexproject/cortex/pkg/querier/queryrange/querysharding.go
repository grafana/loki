package queryrange

import (
	"context"
	fmt "fmt"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/promql"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/cortexproject/cortex/pkg/querier/lazyquery"
)

var (
	nanosecondsInMillisecond = int64(time.Millisecond / time.Nanosecond)

	errInvalidShardingRange = errors.New("Query does not fit in a single sharding configuration")
)

// ShardingConfigs is a slice of chunk shard configs
type ShardingConfigs []chunk.PeriodConfig

// ValidRange extracts a non-overlapping sharding configuration from a list of configs and a time range.
func (confs ShardingConfigs) ValidRange(start, end int64) (chunk.PeriodConfig, error) {
	for i, conf := range confs {
		if start < int64(conf.From.Time) {
			// the query starts before this config's range
			return chunk.PeriodConfig{}, errInvalidShardingRange
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

	return chunk.PeriodConfig{}, errInvalidShardingRange
}

// GetConf will extract a shardable config corresponding to a request and the shardingconfigs
func (confs ShardingConfigs) GetConf(r Request) (chunk.PeriodConfig, error) {
	conf, err := confs.ValidRange(r.GetStart(), r.GetEnd())

	// query exists across multiple sharding configs
	if err != nil {
		return conf, err
	}

	// query doesn't have shard factor, so don't try to do AST mapping.
	if conf.RowShards < 2 {
		return conf, errors.Errorf("shard factor not high enough: [%d]", conf.RowShards)
	}

	return conf, nil
}

func (confs ShardingConfigs) hasShards() bool {
	for _, conf := range confs {
		if conf.RowShards > 0 {
			return true
		}
	}
	return false
}

func mapQuery(mapper astmapper.ASTMapper, query string) (promql.Node, error) {
	expr, err := promql.ParseExpr(query)
	if err != nil {
		return nil, err
	}
	return mapper.Map(expr)
}

// NewQueryShardMiddleware creates a middleware which downstreams queries after AST mapping and query encoding.
func NewQueryShardMiddleware(
	logger log.Logger,
	engine *promql.Engine,
	confs ShardingConfigs,
	codec Codec,
	minShardingLookback time.Duration,
	metrics *InstrumentMiddlewareMetrics,
	registerer prometheus.Registerer,
) Middleware {

	noshards := !confs.hasShards()

	if noshards {
		level.Warn(logger).Log(
			"middleware", "QueryShard",
			"msg", "no configuration with shard found",
			"confs", fmt.Sprintf("%+v", confs),
		)
		return PassthroughMiddleware
	}

	mapperware := MiddlewareFunc(func(next Handler) Handler {
		return newASTMapperware(confs, next, logger, registerer)
	})

	shardingware := MiddlewareFunc(func(next Handler) Handler {
		return &queryShard{
			confs:  confs,
			next:   next,
			engine: engine,
		}
	})

	return MiddlewareFunc(func(next Handler) Handler {
		return &shardSplitter{
			codec:               codec,
			MinShardingLookback: minShardingLookback,
			shardingware: MergeMiddlewares(
				InstrumentMiddleware("shardingware", metrics),
				mapperware,
				shardingware,
			).Wrap(next),
			now:  time.Now,
			next: InstrumentMiddleware("sharding-bypass", metrics).Wrap(next),
		}
	})

}

type astMapperware struct {
	confs  ShardingConfigs
	logger log.Logger
	next   Handler

	// Metrics.
	registerer            prometheus.Registerer
	mappedASTCounter      prometheus.Counter
	shardedQueriesCounter prometheus.Counter
}

func newASTMapperware(confs ShardingConfigs, next Handler, logger log.Logger, registerer prometheus.Registerer) *astMapperware {
	return &astMapperware{
		confs:      confs,
		logger:     log.With(logger, "middleware", "QueryShard.astMapperware"),
		next:       next,
		registerer: registerer,
		mappedASTCounter: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: "cortex",
			Name:      "frontend_mapped_asts_total",
			Help:      "Total number of queries that have undergone AST mapping",
		}),
		shardedQueriesCounter: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: "cortex",
			Name:      "frontend_sharded_queries_total",
			Help:      "Total number of sharded queries",
		}),
	}
}

func (ast *astMapperware) Do(ctx context.Context, r Request) (Response, error) {
	conf, err := ast.confs.GetConf(r)
	// cannot shard with this timerange
	if err != nil {
		level.Warn(ast.logger).Log("err", err.Error(), "msg", "skipped AST mapper for request")
		return ast.next.Do(ctx, r)
	}

	shardSummer, err := astmapper.NewShardSummer(int(conf.RowShards), astmapper.VectorSquasher, ast.shardedQueriesCounter)
	if err != nil {
		return nil, err
	}

	subtreeFolder := astmapper.NewSubtreeFolder()

	strQuery := r.GetQuery()
	mappedQuery, err := mapQuery(
		astmapper.NewMultiMapper(
			shardSummer,
			subtreeFolder,
		),
		strQuery,
	)

	if err != nil {
		return nil, err
	}

	strMappedQuery := mappedQuery.String()
	level.Debug(ast.logger).Log("msg", "mapped query", "original", strQuery, "mapped", strMappedQuery)
	ast.mappedASTCounter.Inc()

	return ast.next.Do(ctx, r.WithQuery(strMappedQuery))

}

type queryShard struct {
	confs  ShardingConfigs
	next   Handler
	engine *promql.Engine
}

func (qs *queryShard) Do(ctx context.Context, r Request) (Response, error) {
	// since there's no available sharding configuration for this time range,
	// no astmapping has been performed, so skip this middleware.
	if _, err := qs.confs.GetConf(r); err != nil {
		return qs.next.Do(ctx, r)
	}

	queryable := lazyquery.NewLazyQueryable(&ShardedQueryable{r, qs.next})

	qry, err := qs.engine.NewRangeQuery(
		queryable,
		r.GetQuery(),
		TimeFromMillis(r.GetStart()),
		TimeFromMillis(r.GetEnd()),
		time.Duration(r.GetStep())*time.Millisecond,
	)

	if err != nil {
		return nil, err
	}
	res := qry.Exec(ctx)
	extracted, err := FromResult(res)
	if err != nil {
		return nil, err

	}
	return &PrometheusResponse{
		Status: StatusSuccess,
		Data: PrometheusData{
			ResultType: string(res.Value.Type()),
			Result:     extracted,
		},
	}, nil
}

// shardSplitter middleware will only shard appropriate requests that do not extend past the MinShardingLookback interval.
// This is used to send nonsharded requests to the ingesters in order to not overload them.
type shardSplitter struct {
	codec               Codec
	MinShardingLookback time.Duration    // delimiter for splitting sharded vs non-sharded queries
	shardingware        Handler          // handler for sharded queries
	next                Handler          // handler for non-sharded queries
	now                 func() time.Time // injectable time.Now
}

func (splitter *shardSplitter) Do(ctx context.Context, r Request) (Response, error) {
	cutoff := splitter.now().Add(-splitter.MinShardingLookback)
	sharded, nonsharded := partitionRequest(r, cutoff)

	return splitter.parallel(ctx, sharded, nonsharded)

}

func (splitter *shardSplitter) parallel(ctx context.Context, sharded, nonsharded Request) (Response, error) {
	if sharded == nil {
		return splitter.next.Do(ctx, nonsharded)
	}

	if nonsharded == nil {
		return splitter.shardingware.Do(ctx, sharded)
	}

	nonshardCh := make(chan Response, 1)
	shardCh := make(chan Response, 1)
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

	resps := make([]Response, 0, 2)
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

	return splitter.codec.MergeResponse(resps...)
}

// partitionQuery splits a request into potentially multiple requests, one including the request's time range
// [0,t). The other will include [t,inf)
func partitionRequest(r Request, t time.Time) (before Request, after Request) {
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
