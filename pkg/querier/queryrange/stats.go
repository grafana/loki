package queryrange

import (
	"context"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/logql"
)

// StatsMiddleware creates a new Middleware that recompute the stats summary based on the actual duration of the request.
// The middleware also register Prometheus metrics.
func StatsMiddleware() queryrange.Middleware {
	return queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
		return queryrange.HandlerFunc(func(ctx context.Context, req queryrange.Request) (queryrange.Response, error) {
			//todo span logging.
			// opentracing.SpanFromContext(ctx)
			start := time.Now()
			resp, err := next.Do(ctx, req)
			if resp != nil {
				switch r := resp.(type) {
				case *LokiResponse:
				case *LokiPromResponse:
					r.Statistics.ComputeSummary(time.Since(start))
				}
			}
			logql.RecordMetrics(status, req.GetQuery(), rangeType, statResult)
			return resp, err
		})
	})
}
