package congestion

import (
	"context"
	"errors"
	"io"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

var RetriesExceeded = errors.New("retries exceeded")

type NoopRetrier struct{}

func NewNoopRetrier(Config) *NoopRetrier {
	return &NoopRetrier{}
}

func (n *NoopRetrier) Do(fn DoRequestFunc, isRetryable IsRetryableErrFunc, onSuccess func(), onError func()) (io.ReadCloser, int64, error) {
	// don't retry, just execute the given function once, but still report the outcome so a
	// wrapping congestion controller reacts to it — this is what makes it equivalent to
	// LimitedRetrier with limit=0, per the comment below.
	rc, sz, err := fn(0)
	switch {
	case err == nil:
		onSuccess()
	case isRetryable(err):
		onError()
	}

	return rc, sz, err
}

func (n *NoopRetrier) withLogger(log.Logger) Retrier { return n }

// LimitedRetrier executes the initial request plus a configurable limit of subsequent retries.
// limit=0 is equivalent to NoopRetrier
type LimitedRetrier struct {
	limit   int
	logger  log.Logger
	metrics *Metrics
}

func NewLimitedRetrier(cfg Config, metrics *Metrics) *LimitedRetrier {
	return &LimitedRetrier{
		limit:   cfg.Retry.Limit,
		metrics: metrics,
	}
}

func (l *LimitedRetrier) Do(fn DoRequestFunc, isRetryable IsRetryableErrFunc, onSuccess func(), onError func()) (io.ReadCloser, int64, error) {
	// i = 0 is initial request
	// i > 0 is retry
	for i := 0; i <= l.limit; i++ {
		rc, sz, err := fn(i)

		if err != nil {
			if !isRetryable(err) {
				if !errors.Is(err, context.Canceled) {
					l.metrics.nonRetryableErrors.Inc()
					level.Debug(l.logger).Log("msg", "store error is not retryable", "err", err)
				}
				return rc, sz, err
			}

			level.Debug(l.logger).Log("msg", "error is retryable", "err", err)
			// TODO(dannyk): consider this more carefully
			// only decrease rate-limit if error is retryable, otherwise all errors (context cancelled, dial errors, timeouts, etc)
			// which may be mostly client-side would inappropriately reduce throughput
			onError()
			continue
		}

		onSuccess()
		return rc, sz, err
	}

	return nil, 0, RetriesExceeded
}

func (l *LimitedRetrier) withLogger(logger log.Logger) Retrier {
	l.logger = logger
	return l
}
