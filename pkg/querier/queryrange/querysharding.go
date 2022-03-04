package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/tenant"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/marshal"
)

var errInvalidShardingRange = errors.New("Query does not fit in a single sharding configuration")

// NewQueryShardMiddleware creates a middleware which downstreams queries after AST mapping and query encoding.
func NewQueryShardMiddleware(
	logger log.Logger,
	confs ShardingConfigs,
	middlewareMetrics *queryrangebase.InstrumentMiddlewareMetrics,
	shardingMetrics *logql.ShardingMetrics,
	limits Limits,
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
		return newASTMapperware(confs, next, logger, shardingMetrics, limits)
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
	next queryrangebase.Handler,
	logger log.Logger,
	metrics *logql.ShardingMetrics,
	limits logql.Limits,
) *astMapperware {
	return &astMapperware{
		confs:   confs,
		logger:  log.With(logger, "middleware", "QueryShard.astMapperware"),
		next:    next,
		ng:      logql.NewShardedEngine(logql.EngineOpts{}, DownstreamHandler{next}, metrics, limits, logger),
		metrics: metrics,
	}
}

type astMapperware struct {
	confs   ShardingConfigs
	logger  log.Logger
	next    queryrangebase.Handler
	ng      *logql.ShardedEngine
	metrics *logql.ShardingMetrics
}

func (ast *astMapperware) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	conf, err := ast.confs.GetConf(r)
	logger := util_log.WithContext(ctx, ast.logger)
	// cannot shard with this timerange
	if err != nil {
		level.Warn(logger).Log("err", err.Error(), "msg", "skipped AST mapper for request")
		return ast.next.Do(ctx, r)
	}

	mapper, err := logql.NewShardMapper(int(conf.RowShards), ast.metrics)
	if err != nil {
		return nil, err
	}

	noop, parsed, err := mapper.Parse(r.GetQuery())
	if err != nil {
		level.Warn(logger).Log("msg", "failed mapping AST", "err", err.Error(), "query", r.GetQuery())
		return nil, err
	}
	level.Debug(logger).Log("no-op", noop, "mapped", parsed.String())

	if noop {
		// the ast can't be mapped to a sharded equivalent
		// so we can bypass the sharding engine.
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
			Response: &queryrangebase.PrometheusResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: queryrangebase.PrometheusData{
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
		return &LokiPromResponse{
			Statistics: res.Statistics,
			Response: &queryrangebase.PrometheusResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: queryrangebase.PrometheusData{
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
	limits       Limits                 // delimiter for splitting sharded vs non-sharded queries
	shardingware queryrangebase.Handler // handler for sharded queries
	next         queryrangebase.Handler // handler for non-sharded queries
	now          func() time.Time       // injectable time.Now
}

func (splitter *shardSplitter) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	userid, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}
	minShardingLookback := splitter.limits.MinShardingLookback(userid)
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
		if conf.RowShards > 0 {
			return true
		}
	}
	return false
}

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
func (confs ShardingConfigs) GetConf(r queryrangebase.Request) (chunk.PeriodConfig, error) {
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

// NewSeriesQueryShardMiddleware creates a middleware which shards series queries.
func NewSeriesQueryShardMiddleware(
	logger log.Logger,
	confs ShardingConfigs,
	middlewareMetrics *queryrangebase.InstrumentMiddlewareMetrics,
	shardingMetrics *logql.ShardingMetrics,
	limits queryrangebase.Limits,
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
	metrics *logql.ShardingMetrics
	limits  queryrangebase.Limits
	merger  queryrangebase.Merger
}

func (ss *seriesShardingHandler) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
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

	requests := make([]queryrangebase.Request, 0, conf.RowShards)
	for i := 0; i < int(conf.RowShards); i++ {
		shardedRequest := *req
		shardedRequest.Shards = []string{astmapper.ShardAnnotation{
			Shard: i,
			Of:    int(conf.RowShards),
		}.String()}
		requests = append(requests, &shardedRequest)
	}
	requestResponses, err := queryrangebase.DoRequests(ctx, ss.next, requests, ss.limits)
	if err != nil {
		return nil, err
	}
	responses := make([]queryrangebase.Response, 0, len(requestResponses))
	for _, res := range requestResponses {
		responses = append(responses, res.Response)
	}
	return ss.merger.MergeResponse(responses...)
}
