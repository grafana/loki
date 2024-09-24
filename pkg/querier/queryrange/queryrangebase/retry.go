package queryrangebase

import (
	"context"
	"reflect"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/grpcutil"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

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
	if metrics == nil {
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
	defer func() { r.metrics.retriesCount.Observe(float64(tries)) }()

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
		// Make sure the context isn't done before sending the request
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		resp, err := r.next.Do(ctx, req)
		if err == nil {
			return resp, nil
		}

		// Make sure the context isn't done before retrying the request
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		code := grpcutil.ErrorToStatusCode(err)
		// Error handling is tricky... There are many places we wrap any error and set an HTTP style status code
		// but there are also places where we return an existing GRPC object which will use GRPC status codes
		// If the code is < 100 it's a gRPC status code, currently we retry all of these, even codes.Canceled
		// because when our pools close connections they do so with a cancel and we want to retry these
		// If it's > 100, it's an HTTP code and we only retry 5xx
		if code < 100 || code/100 == 5 {
			lastErr = err
			level.Error(util_log.WithContext(ctx, r.log)).Log(
				"msg", "error processing request",
				"try", tries,
				"type", logImplementingType(req),
				"query", query,
				"query_hash", util.HashedQuery(query),
				"start", start.Format(time.RFC3339Nano),
				"end", end.Format(time.RFC3339Nano),
				"start_delta", time.Since(start),
				"end_delta", time.Since(end),
				"length", end.Sub(start),
				"retry_in", bk.NextDelay(),
				"code", code,
				"err", err,
			)
			bk.Wait()
			continue
		} else {
			level.Warn(util_log.WithContext(ctx, r.log)).Log("msg", "received an error but not a retryable code, this is possibly a bug.", "code", code, "err", err)
		}

		return nil, err
	}
	return nil, lastErr
}

func logImplementingType(i Request) string {
	if i == nil {
		return "nil"
	}

	t := reflect.TypeOf(i)

	// Check if it's a pointer and get the underlying type if so
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	return t.String()
}
