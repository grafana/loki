package queryrange

import (
	"context"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/httpgrpc"
)

var retries = promauto.NewHistogram(prometheus.HistogramOpts{
	Namespace: "cortex",
	Name:      "query_frontend_retries",
	Help:      "Number of times a request is retried.",
	Buckets:   []float64{0, 1, 2, 3, 4, 5},
})

type retry struct {
	log        log.Logger
	next       Handler
	maxRetries int
}

// NewRetryMiddleware returns a middleware that retries requests if they
// fail with 500 or a non-HTTP error.
func NewRetryMiddleware(log log.Logger, maxRetries int) Middleware {
	return MiddlewareFunc(func(next Handler) Handler {
		return retry{
			log:        log,
			next:       next,
			maxRetries: maxRetries,
		}
	})
}

func (r retry) Do(ctx context.Context, req *Request) (*APIResponse, error) {
	tries := 0
	defer func() { retries.Observe(float64(tries)) }()

	var lastErr error
	for ; tries < r.maxRetries; tries++ {
		resp, err := r.next.Do(ctx, req)
		if err == nil {
			return resp, nil
		}

		// Retry if we get a HTTP 500 or a non-HTTP error.
		httpResp, ok := httpgrpc.HTTPResponseFromError(err)
		if !ok || httpResp.Code/100 == 5 {
			lastErr = err
			level.Error(r.log).Log("msg", "error processing request", "try", tries, "err", err)
			continue
		}

		return nil, err
	}
	return nil, lastErr
}
