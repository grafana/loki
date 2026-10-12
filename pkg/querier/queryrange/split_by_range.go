package queryrange

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/marshal"
)

// splitByRange splits the range vector of an instant metric request into sub-queries. The split
// interval comes from the chunk policy or from the data-object policy, whichever fits the request.
type splitByRange struct {
	logger        log.Logger
	next          queryrangebase.Handler
	limits        Limits
	chunkPolicy   chunkSplitIntervalPolicy
	dataObjPolicy *dataObjSplitIntervalPolicy
	ng            *logql.DownstreamEngine
	metrics       *logql.MapperMetrics

	// Whether to align rangeInterval align to splitByInterval in the subqueries.
	splitAlign bool
}

// NewSplitByRangeMiddleware creates a new Middleware that splits log requests by the range interval.
func NewSplitByRangeMiddleware(logger log.Logger, cfg Config, engineOpts logql.EngineOpts, limits Limits, splitAlign bool, metrics *logql.MapperMetrics) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &splitByRange{
			logger:        log.With(logger, "middleware", "InstantQuery.splitByRangeVector"),
			next:          next,
			limits:        limits,
			chunkPolicy:   newChunkSplitIntervalPolicy(limits),
			dataObjPolicy: newDataObjSplitIntervalPolicy(cfg, limits),
			ng: logql.NewDownstreamEngine(engineOpts, DownstreamHandler{
				limits:     limits,
				next:       next,
				splitAlign: splitAlign,
			}, limits, logger),
			metrics:    metrics,
			splitAlign: splitAlign,
		}
	})
}

// policy returns the split policy of the request.
func (s *splitByRange) policy(tenantIDs []string, request queryrangebase.Request) splitIntervalPolicy {
	req, ok := request.(*LokiInstantRequest)
	if !ok {
		// The router sends only instant requests to this middleware, so this is not expected in
		// practice.
		return s.chunkPolicy
	}

	expr, err := planExpr(req.Plan, req.Query)
	if err == nil && s.dataObjPolicy.shouldUse(tenantIDs, expr, req.TimeTs, req.TimeTs) {
		return s.dataObjPolicy
	}

	return s.chunkPolicy
}

func (s *splitByRange) Do(ctx context.Context, request queryrangebase.Request) (queryrangebase.Response, error) {
	logger := util_log.WithContext(ctx, s.logger)

	params, err := ParamsFromRequest(request)
	if err != nil {
		return nil, err
	}

	tenants, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	interval := s.policy(tenants, request).instantQueryInterval(tenants)
	// if no interval configured, continue to the next middleware
	if interval == 0 {
		return s.next.Do(ctx, request)
	}

	mapperStats := logql.NewMapperStats()

	ir, ok := request.(*LokiInstantRequest)
	if !ok {
		return nil, fmt.Errorf("expected *LokiInstantRequest, got %T", request)
	}

	var mapper logql.RangeMapper

	if s.splitAlign {
		mapper, err = logql.NewRangeMapperWithSplitAlign(interval, ir.TimeTs, s.metrics, mapperStats)
	} else {
		mapper, err = logql.NewRangeMapper(interval, s.metrics, mapperStats)
	}
	if err != nil {
		return nil, err
	}

	noop, parsed, err := mapper.Parse(params.GetExpression())
	if err != nil {
		level.Warn(logger).Log("msg", "failed mapping AST", "err", err.Error(), "query", request.GetQuery())
		return nil, err
	}
	level.Debug(logger).Log("msg", "mapped instant query", "interval", interval.String(), "noop", noop, "original", request.GetQuery(), "mapped", parsed.String())

	if noop {
		// the query cannot be split, so continue
		return s.next.Do(ctx, request)
	}

	// Update middleware stats
	queryStatsCtx := stats.FromContext(ctx)
	queryStatsCtx.AddSplitQueries(int64(mapperStats.GetSplitQueries()))

	query := s.ng.Query(ctx, logql.ParamsWithExpressionOverride{Params: params, ExpressionOverride: parsed})

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
	case parser.ValueTypeVector:
		return &LokiPromResponse{
			Response: &queryrangebase.PrometheusResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: queryrangebase.PrometheusData{
					ResultType: loghttp.ResultTypeVector,
					Result:     toProtoVector(value.(loghttp.Vector)),
				},
			},
			Statistics: res.Statistics,
		}, nil
	default:
		return nil, fmt.Errorf("unexpected downstream response type (%T)", res.Data.Type())
	}
}
