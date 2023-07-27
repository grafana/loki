package queryrange

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/marshal"
	"github.com/grafana/loki/pkg/util/validation"
)

type splitByRange struct {
	logger  log.Logger
	next    queryrangebase.Handler
	limits  Limits
	ng      *logql.DownstreamEngine
	metrics *logql.MapperMetrics
}

// NewSplitByRangeMiddleware creates a new Middleware that splits log requests by the range interval.
func NewSplitByRangeMiddleware(logger log.Logger, engineOpts logql.EngineOpts, limits Limits, metrics *logql.MapperMetrics) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &splitByRange{
			logger: log.With(logger, "middleware", "InstantQuery.splitByRangeVector"),
			next:   next,
			limits: limits,
			ng: logql.NewDownstreamEngine(engineOpts, DownstreamHandler{
				limits: limits,
				next:   next,
			}, limits, logger),
			metrics: metrics,
		}
	})
}

func (s *splitByRange) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	logger := util_log.WithContext(ctx, s.logger)

	tenants, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	interval := validation.SmallestPositiveNonZeroDurationPerTenant(tenants, s.limits.QuerySplitDuration)
	// if no interval configured, continue to the next middleware
	if interval == 0 {
		return s.next.Do(ctx, req)
	}

	mapperStats := logql.NewMapperStats()
	mapper, err := logql.NewRangeMapper(interval, s.metrics, mapperStats)
	if err != nil {
		return nil, err
	}

	noop, parsed, err := mapper.Parse(req.GetQuery())
	if err != nil {
		level.Warn(logger).Log("msg", "failed mapping AST", "err", err.Error(), "query", req.GetQuery())
		return nil, err
	}
	level.Debug(logger).Log("msg", "mapped instant query", "interval", interval.String(), "noop", noop, "original", req.GetQuery(), "mapped", parsed.String())

	if noop {
		// the query cannot be split, so continue
		return s.next.Do(ctx, req)
	}

	// Update middleware stats
	queryStatsCtx := stats.FromContext(ctx)
	queryStatsCtx.AddSplitQueries(int64(mapperStats.GetSplitQueries()))

	params, err := paramsFromRequest(req)
	if err != nil {
		return nil, err
	}

	if _, ok := req.(*LokiInstantRequest); !ok {
		return nil, fmt.Errorf("expected *LokiInstantRequest")
	}

	query := s.ng.Query(ctx, params, parsed)

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
