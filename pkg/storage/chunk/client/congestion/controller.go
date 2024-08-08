package congestion

import (
	"context"
	"errors"
	"io"
	"math"
	"time"

	"github.com/go-kit/log"
	"golang.org/x/time/rate"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
)

// AIMDController implements the Additive-Increase/Multiplicative-Decrease algorithm which is used in TCP congestion avoidance.
// https://en.wikipedia.org/wiki/Additive_increase/multiplicative_decrease
type AIMDController struct {
	inner client.ObjectClient

	retrier Retrier
	hedger  Hedger
	metrics *Metrics

	limiter       *rate.Limiter
	backoffFactor float64
	upperBound    rate.Limit

	logger log.Logger
}

func NewAIMDController(cfg Config) *AIMDController {
	lowerBound := rate.Limit(cfg.Controller.AIMD.Start)
	upperBound := rate.Limit(cfg.Controller.AIMD.UpperBound)

	if lowerBound <= 0 {
		lowerBound = 1
	}

	if upperBound <= 0 {
		// set to infinity if not defined
		upperBound = rate.Limit(math.Inf(1))
	}

	backoffFactor := cfg.Controller.AIMD.BackoffFactor
	if backoffFactor <= 0 {
		// AIMD algorithm calls for halving rate
		backoffFactor = 0.5
	}

	return &AIMDController{
		limiter:       rate.NewLimiter(lowerBound, int(lowerBound)),
		backoffFactor: backoffFactor,
		upperBound:    upperBound,
	}
}

func (a *AIMDController) Wrap(client client.ObjectClient) client.ObjectClient {
	a.inner = client
	return a
}

func (a *AIMDController) withRetrier(r Retrier) Controller {
	a.retrier = r
	return a
}

func (a *AIMDController) withHedger(h Hedger) Controller {
	a.hedger = h
	return a
}

func (a *AIMDController) withMetrics(m *Metrics) Controller {
	a.metrics = m

	a.updateLimitMetric()
	return a
}

func (a *AIMDController) withLogger(logger log.Logger) Controller {
	a.logger = logger
	return a
}

func (a *AIMDController) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	return a.inner.PutObject(ctx, objectKey, object)
}

func (a *AIMDController) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	// Only GetObject implements congestion avoidance; the other methods are either non-idempotent which means they
	// cannot be retried, or are too low volume to care about

	// TODO(dannyk): use hedging client to handle requests, do NOT hedge retries

	start := time.Now()
	statsCtx := stats.FromContext(ctx)

	rc, sz, err := a.retrier.Do(
		func(attempt int) (io.ReadCloser, int64, error) {
			a.metrics.requests.Add(1)

			// in retry
			if attempt > 0 {
				a.metrics.retries.Add(1)
			}

			// apply back-pressure while rate-limit has been exceeded
			//
			// using Reserve() is slower because it assumes a constant wait time as tokens are replenished, but in experimentation
			// it's faster to sit in a hot loop and probe every so often if there are tokens available
			for !a.limiter.Allow() {
				delay := time.Millisecond * 10
				time.Sleep(delay)
				a.metrics.backoffSec.Add(delay.Seconds())
			}

			statsCtx.AddCongestionControlLatency(time.Since(start))

			// It is vitally important that retries are DISABLED in the inner implementation.
			// Some object storage clients implement retries internally, and this will interfere here.
			return a.inner.GetObject(ctx, objectKey)
		},
		a.IsRetryableErr,
		a.additiveIncrease,
		a.multiplicativeDecrease,
	)

	if errors.Is(err, RetriesExceeded) {
		a.metrics.retriesExceeded.Add(1)
	}

	return rc, sz, err
}

func (a *AIMDController) GetObjectRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	return a.inner.GetObjectRange(ctx, objectKey, offset, length)
}

func (a *AIMDController) List(ctx context.Context, prefix string, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	return a.inner.List(ctx, prefix, delimiter)
}

func (a *AIMDController) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	return a.inner.ObjectExists(ctx, objectKey)
}

func (a *AIMDController) DeleteObject(ctx context.Context, objectKey string) error {
	return a.inner.DeleteObject(ctx, objectKey)
}

func (a *AIMDController) IsObjectNotFoundErr(err error) bool {
	return a.inner.IsObjectNotFoundErr(err)
}

func (a *AIMDController) IsRetryableErr(err error) bool {
	retryable := a.inner.IsRetryableErr(err)
	if !retryable {
		if !errors.Is(err, context.Canceled) {
			a.metrics.nonRetryableErrors.Inc()
		}
	}

	return retryable
}

func (a *AIMDController) Stop() {
	a.inner.Stop()
}

// additiveIncrease increases the number of requests per second that can be sent linearly.
// it should never exceed the defined upper bound.
func (a *AIMDController) additiveIncrease() {
	newLimit := a.limiter.Limit() + 1

	if newLimit > a.upperBound {
		newLimit = a.upperBound
	}

	a.limiter.SetLimit(newLimit)
	a.limiter.SetBurst(int(newLimit))

	a.updateLimitMetric()
}

// multiplicativeDecrease reduces the number of requests per second that can be sent exponentially.
// it should never be set lower than 1.
func (a *AIMDController) multiplicativeDecrease() {
	newLimit := math.Ceil(math.Max(1, float64(a.limiter.Limit())*a.backoffFactor))

	a.limiter.SetLimit(rate.Limit(newLimit))
	a.limiter.SetBurst(int(newLimit))

	a.updateLimitMetric()
}

func (a *AIMDController) updateLimitMetric() {
	a.metrics.currentLimit.Set(float64(a.limiter.Limit()))
}
func (a *AIMDController) getRetrier() Retrier  { return a.retrier }
func (a *AIMDController) getHedger() Hedger    { return a.hedger }
func (a *AIMDController) getMetrics() *Metrics { return a.metrics }

type NoopController struct {
	retrier Retrier
	hedger  Hedger
	metrics *Metrics
	logger  log.Logger
}

func NewNoopController(Config) *NoopController {
	return &NoopController{}
}

func (n *NoopController) ObjectExists(context.Context, string) (bool, error) { return true, nil }
func (n *NoopController) PutObject(context.Context, string, io.Reader) error { return nil }
func (n *NoopController) GetObject(context.Context, string) (io.ReadCloser, int64, error) {
	return nil, 0, nil
}
func (n *NoopController) GetObjectRange(context.Context, string, int64, int64) (io.ReadCloser, error) {
	return nil, nil
}

func (n *NoopController) List(context.Context, string, string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	return nil, nil, nil
}
func (n *NoopController) DeleteObject(context.Context, string) error     { return nil }
func (n *NoopController) IsObjectNotFoundErr(error) bool                 { return false }
func (n *NoopController) IsRetryableErr(error) bool                      { return false }
func (n *NoopController) Stop()                                          {}
func (n *NoopController) Wrap(c client.ObjectClient) client.ObjectClient { return c }

func (n *NoopController) withLogger(logger log.Logger) Controller {
	n.logger = logger
	return n
}

func (n *NoopController) withRetrier(r Retrier) Controller {
	n.retrier = r
	return n
}

func (n *NoopController) withHedger(h Hedger) Controller {
	n.hedger = h
	return n
}

func (n *NoopController) withMetrics(m *Metrics) Controller {
	n.metrics = m
	return n
}
func (n *NoopController) getRetrier() Retrier  { return n.retrier }
func (n *NoopController) getHedger() Hedger    { return n.hedger }
func (n *NoopController) getMetrics() *Metrics { return n.metrics }
