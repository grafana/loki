package queryrange

import (
	"context"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/httpgrpc"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
)

type RetryMiddlewareMetrics struct {
	retriesCount prometheus.Histogram
}

func NewRetryMiddlewareMetrics(registerer prometheus.Registerer) *RetryMiddlewareMetrics {
	return &RetryMiddlewareMetrics{
		retriesCount: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: "cortex",
			Name:      "query_frontend_retries",
			Help:      "Number of times a request is retried.",
			Buckets:   []float64{0, 1, 2, 3, 4, 5},
		}),
	}
}

type retry struct {
	log        log.Logger
	next       Handler
	maxRetries int

	metrics *RetryMiddlewareMetrics
}

// NewRetryMiddleware returns a middleware that retries requests if they
// fail with 500 or a non-HTTP error.
func NewRetryMiddleware(log log.Logger, maxRetries int, metrics *RetryMiddlewareMetrics) Middleware {
	if metrics == nil {
		metrics = NewRetryMiddlewareMetrics(nil)
	}

	return MiddlewareFunc(func(next Handler) Handler {
		return retry{
			log:        log,
			next:       next,
			maxRetries: maxRetries,
			metrics:    metrics,
		}
	})
}

func (r retry) Do(ctx context.Context, req Request) (Response, error) {
	tries := 0
	defer func() { r.metrics.retriesCount.Observe(float64(tries)) }()

	var lastErr error
	for ; tries < r.maxRetries; tries++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		resp, err := r.next.Do(ctx, req)
		if err == nil {
			return resp, nil
		}

		// Retry if we get a HTTP 500 or a non-HTTP error.
		httpResp, ok := httpgrpc.HTTPResponseFromError(err)
		if !ok || httpResp.Code/100 == 5 {
			lastErr = err
			level.Error(util_log.WithContext(ctx, r.log)).Log("msg", "error processing request", "try", tries, "err", err)
			continue
		}

		return nil, err
	}
	return nil, lastErr
}
