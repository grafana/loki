package queryrange

import (
	"context"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
)

// StatsMiddleware creates a new Middleware that recompute the stats summary based on the actual duration of the request.
// The middleware also register Prometheus metrics.
func StatsMiddleware() queryrange.Middleware {
	return queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
		return queryrange.HandlerFunc(func(ctx context.Context, req queryrange.Request) (queryrange.Response, error) {
			//todo span logging.
			start := time.Now()
			resp, err := next.Do(ctx, req)
			if resp != nil {
				switch r := resp.(type) {
				case *LokiResponse:
				case *LokiPromResponse:
					r.Statistics.ComputeSummary(time.Since(start))
				}
			}
			return resp, err
		})
	})
}
