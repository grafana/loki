package queryrange

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"
)

// InstrumentMiddleware can be inserted into the middleware chain to expose timing information.
func InstrumentMiddleware(name string, queryRangeDuration *prometheus.HistogramVec) Middleware {
	return MiddlewareFunc(func(next Handler) Handler {
		return HandlerFunc(func(ctx context.Context, req *Request) (*APIResponse, error) {
			var resp *APIResponse
			err := instrument.TimeRequestHistogram(ctx, name, queryRangeDuration, func(ctx context.Context) error {
				var err error
				resp, err = next.Do(ctx, req)
				return err
			})
			return resp, err
		})
	})
}
