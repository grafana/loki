package queryrange

import (
	"context"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log/level"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/stats"
)

var defaultMetricRecorder = metricRecorderFn(func(status, query string, rangeType logql.QueryRangeType, stats stats.Result) {
	logql.RecordMetrics(status, query, rangeType, stats)
})

type metricRecorder interface {
	Record(status, query string, rangeType logql.QueryRangeType, stats stats.Result)
}

type metricRecorderFn func(status, query string, rangeType logql.QueryRangeType, stats stats.Result)

func (m metricRecorderFn) Record(status, query string, rangeType logql.QueryRangeType, stats stats.Result) {
	m(status, query, rangeType, stats)
}

// StatsMiddleware creates a new Middleware that recompute the stats summary based on the actual duration of the request.
// The middleware also register Prometheus metrics.
func StatsMiddleware() queryrange.Middleware {
	return statsMiddleware(defaultMetricRecorder)
}

func statsMiddleware(recorder metricRecorder) queryrange.Middleware {
	return queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
		return queryrange.HandlerFunc(func(ctx context.Context, req queryrange.Request) (queryrange.Response, error) {
			logger := spanlogger.FromContext(ctx)
			start := time.Now()

			// execute the request
			resp, err := next.Do(ctx, req)

			// collect stats and status
			var statistics *stats.Result
			var status string
			if resp != nil {
				switch r := resp.(type) {
				case *LokiResponse:
					statistics = &r.Statistics
					status = r.Status
				case *LokiPromResponse:
					statistics = &r.Statistics
					if r.Response != nil {
						status = r.Response.Status
					}
				default:
					level.Warn(util.Logger).Log("msg", fmt.Sprintf("cannot compute stats, unexpected type: %T", resp))
				}
			}
			if err != nil {
				status = loghttp.QueryStatusFail
			}
			if statistics != nil {
				// Re-calculate the summary then log and record metrics for the current query
				statistics.ComputeSummary(time.Since(start))
				statistics.Log(logger)
				recorder.Record(status, req.GetQuery(), logql.GetRangeType(paramsFromRequest(req)), *statistics)
			}
			return resp, err
		})
	})
}
