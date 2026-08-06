package congestion

import (
	"context"
	"errors"
	"io"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
)

var RetriesExceeded = errors.New("retries exceeded")

type NoopRetrier struct{}

func NewNoopRetrier(Config) *NoopRetrier {
	return &NoopRetrier{}
}

func (n *NoopRetrier) Do(_ context.Context, fn DoRequestFunc, _ IsRetryableErrFunc, _ func(), _ func()) (io.ReadCloser, int64, error) {
	// don't retry, just execute the given function once
	return fn(0)
}

func (n *NoopRetrier) withLogger(log.Logger) Retrier { return n }

// LimitedRetrier executes the initial request plus a configurable limit of subsequent retries.
// A limit of 0 returns a bare RetriesExceeded and erases the original error. For that
// reason, Config.ReplacesInnerRetries requires a limit above 0.
type LimitedRetrier struct {
	limit   int
	backoff backoff.Config
	logger  log.Logger
	metrics *Metrics
}

func NewLimitedRetrier(cfg Config, metrics *Metrics) *LimitedRetrier {
	return &LimitedRetrier{
		limit:   cfg.Retry.Limit,
		metrics: metrics,
		backoff: backoff.Config{
			MinBackoff: cfg.Retry.BackoffMinPeriod,
			MaxBackoff: cfg.Retry.BackoffMaxPeriod,
			// l.limit bounds the attempt count. A second limit here truncates the
			// configured retry budget.
			MaxRetries: 0,
		},
	}
}

func (l *LimitedRetrier) Do(ctx context.Context, fn DoRequestFunc, isRetryable IsRetryableErrFunc, onSuccess func(), onError func()) (io.ReadCloser, int64, error) {
	var bk *backoff.Backoff

	// i = 0 is initial request
	// i > 0 is retry
	for i := 0; i <= l.limit; i++ {
		if i > 0 {
			if bk == nil {
				bk = backoff.New(ctx, l.backoff)
			}
			bk.Wait()
			if err := ctx.Err(); err != nil {
				return nil, 0, err
			}
		}

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
