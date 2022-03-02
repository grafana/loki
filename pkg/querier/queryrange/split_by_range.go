package queryrange

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/tenant"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/marshal"
	"github.com/grafana/loki/pkg/util/validation"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/weaveworks/common/httpgrpc"
)

type splitByRange struct {
	logger log.Logger
	next   queryrangebase.Handler
	limits Limits

	ng *logql.ShardedEngine
}

// SplitByRangeMiddleware creates a new Middleware that splits log requests by the range interval.
func SplitByRangeMiddleware(logger log.Logger, limits Limits, metrics *logql.ShardingMetrics) (queryrangebase.Middleware, error) {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &splitByRange{
			logger: log.With(logger, "middleware", "QueryShard.splitByRange"),
			next:   next,
			limits: limits,
			ng:     logql.NewShardedEngine(logql.EngineOpts{}, DownstreamHandler{next}, metrics, limits, logger),
		}
	}), nil
}

func (s *splitByRange) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	logger := util_log.WithContext(ctx, s.logger)

	tenants, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	interval := validation.SmallestPositiveNonZeroDurationPerTenant(tenants, s.limits.QuerySplitDuration)
	// if no interval configured, continue to the next middleware
	if interval == 0 {
		return s.next.Do(ctx, r)
	}

	mapper, err := logql.NewRangeVectorMapper(interval)
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
		// the query cannot be sharded, so continue
		return s.next.Do(ctx, r)
	}

	params, err := paramsFromRequest(r)
	if err != nil {
		return nil, err
	}

	// todo change to an if statement
	switch r := r.(type) {
	case *LokiInstantRequest:
	default:
		return nil, fmt.Errorf("expected *LokiRequest or *LokiInstantRequest, got (%T)", r)
	}
	query := s.ng.Query(params, parsed)

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
