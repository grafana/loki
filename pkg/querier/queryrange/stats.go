package queryrange

import (
	"context"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/stats"
)

// RecordMetrics is the function used to record metrics, overridden during tests.
var RecordMetrics = func(status, query string, rangeType logql.QueryRangeType, stats stats.Result) {
	logql.RecordMetrics(status, query, rangeType, stats)
}

// StatsMiddleware creates a new Middleware that recompute the stats summary based on the actual duration of the request.
// The middleware also register Prometheus metrics.
func StatsMiddleware() queryrange.Middleware {
	return queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
		return queryrange.HandlerFunc(func(ctx context.Context, req queryrange.Request) (queryrange.Response, error) {
			logger := spanlogger.FromContext(ctx)
			start := time.Now()

			// execute the request
			resp, err := next.Do(ctx, req)

			// collect stats and status
			var statistics stats.Result
			var status string
			if resp != nil {
				switch r := resp.(type) {
				case *LokiResponse:
					statistics = r.Statistics
					status = r.Status
				case *LokiPromResponse:
					statistics = r.Statistics
					if r.Response != nil {
						status = r.Response.Status
					}
				}
			}
			if err != nil {
				status = loghttp.QueryStatusFail
			}
			// Re-calculate the summary then log and record metrics for the current query
			statistics.ComputeSummary(time.Since(start))
			statistics.Log(logger)
			RecordMetrics(status, req.GetQuery(), logql.GetRangeType(paramsFromRequest(req)), statistics)
			return resp, err
		})
	})
}
