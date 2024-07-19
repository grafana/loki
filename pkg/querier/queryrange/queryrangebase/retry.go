package queryrangebase

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/grpcutil"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc/codes"

	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type RetryMiddlewareMetrics struct {
	retriesCount prometheus.Histogram
}

func NewRetryMiddlewareMetrics(registerer prometheus.Registerer, metricsNamespace string) *RetryMiddlewareMetrics {
	return &RetryMiddlewareMetrics{
		retriesCount: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
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
func NewRetryMiddleware(log log.Logger, maxRetries int, metrics *RetryMiddlewareMetrics, metricsNamespace string) Middleware {
	if metrics == nil && metricsNamespace != "" {
		metrics = NewRetryMiddlewareMetrics(nil, metricsNamespace)
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
	if r.metrics != nil {
		defer func() { r.metrics.retriesCount.Observe(float64(tries)) }()
	}

	var lastErr error

	// For the default of 5 tries
	// try 0: no delay
	// try 1: 250ms wait
	// try 2: 500ms wait
	// try 3: 1s wait
	// try 4: 2s wait

	cfg := backoff.Config{
		MinBackoff: 250 * time.Millisecond,
		MaxBackoff: 5 * time.Second,
		MaxRetries: 0,
	}
	bk := backoff.New(ctx, cfg)

	start := req.GetStart()
	end := req.GetEnd()
	query := req.GetQuery()

	for ; tries < r.maxRetries; tries++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		resp, err := r.next.Do(ctx, req)
		if err == nil {
			return resp, nil
		}

		// Retry if we get a HTTP 500 or an unknown error.
		if code := grpcutil.ErrorToStatusCode(err); code == codes.Unknown || code/100 == 5 {
			lastErr = err
			level.Error(util_log.WithContext(ctx, r.log)).Log(
				"msg", "error processing request",
				"try", tries,
				"query", query,
				"query_hash", util.HashedQuery(query),
				"start", start.Format(time.RFC3339Nano),
				"end", end.Format(time.RFC3339Nano),
				"start_delta", time.Since(start),
				"end_delta", time.Since(end),
				"length", end.Sub(start),
				"retry_in", bk.NextDelay(),
				"err", err,
			)
			bk.Wait()
			continue
		}

		return nil, err
	}
	return nil, lastErr
}
