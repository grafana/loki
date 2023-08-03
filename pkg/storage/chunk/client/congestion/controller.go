package congestion

import (
	"context"
	"io"
	"math"
	"time"

	"golang.org/x/time/rate"

	"github.com/grafana/loki/pkg/storage/chunk/client"
)

// AIMDController implements the Additive-Increase/Multiplicative-Decrease algorithm which is used in TCP congestion avoidance.
// https://en.wikipedia.org/wiki/Additive_increase/multiplicative_decrease
type AIMDController struct {
	inner client.ObjectClient

	retry   Retrier
	hedge   Hedger
	metrics *Metrics

	limiter       *rate.Limiter
	backoffFactor float64
	upperBound    rate.Limit
}

func NewAIMDController(cfg Config) *AIMDController {
	lowerBound := rate.Limit(cfg.Controller.AIMD.LowerBound)
	upperBound := rate.Limit(cfg.Controller.AIMD.UpperBound)

	if upperBound == 0 {
		// set to infinity if not defined
		upperBound = rate.Limit(math.Inf(1))
	}

	backoffFactor := cfg.Controller.AIMD.BackoffFactor
	if backoffFactor == 0 {
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

func (a *AIMDController) WithRetrier(r Retrier) Controller {
	a.retry = r
	return a
}

func (a *AIMDController) WithHedger(h Hedger) Controller {
	a.hedge = h
	return a
}

func (a *AIMDController) WithMetrics(m *Metrics) Controller {
	a.metrics = m

	a.updateLimitMetric()
	return a
}

func (a *AIMDController) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return a.inner.PutObject(ctx, objectKey, object)
}

func (a *AIMDController) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	// Only GetObject implements congestion avoidance; the other methods are either non-idempotent which means they
	// cannot be retried, or are too low volume to care

	rc, sz, err := a.retry.Do(
		func(attempt int) (io.ReadCloser, int64, error) {
			// in retry
			if attempt > 0 {
				// TODO(dannyk): define SetHTTPClient on client.ObjectClient interface
				// then use NoopHedger to avoid hedging retries
			}

			// apply back-pressure while rate-limit has been exceeded
			backoffStart := time.Now()
			for {
				// using Reserve() is slower because it assumes a constant wait time as tokens are replenished, but in experimentation
				// it's faster to sit in a hot loop and probe every so often if there are tokens available
				if !a.limiter.Allow() {
					time.Sleep(time.Millisecond * 10)
					continue
				}

				a.metrics.backoffTimeNs.Add(float64(time.Since(backoffStart).Nanoseconds()))
				break
			}

			// It is vitally important that retries are DISABLED in the inner implementation.
			// Some object storage clients implement retries internally, and this will interfere here.
			return a.inner.GetObject(ctx, objectKey)
		},
		a.inner.IsRetryableErr,
		a.additiveIncrease,
		a.multiplicativeDecrease,
	)

	return rc, sz, err
}

func (a *AIMDController) List(ctx context.Context, prefix string, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	return a.inner.List(ctx, prefix, delimiter)
}

func (a *AIMDController) DeleteObject(ctx context.Context, objectKey string) error {
	return a.inner.DeleteObject(ctx, objectKey)
}

func (a *AIMDController) IsObjectNotFoundErr(err error) bool {
	return a.inner.IsObjectNotFoundErr(err)
}

func (a *AIMDController) IsRetryableErr(err error) bool {
	return a.inner.IsRetryableErr(err)
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

type NoopController struct {
	retrier Retrier
	hedger  Hedger
	metrics *Metrics
}

func NewNoopController(Config) *NoopController {
	return &NoopController{}
}

func (n *NoopController) PutObject(context.Context, string, io.ReadSeeker) error { return nil }
func (n *NoopController) GetObject(context.Context, string) (io.ReadCloser, int64, error) {
	return nil, 0, nil
}
func (n *NoopController) List(context.Context, string, string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	return nil, nil, nil
}
func (n *NoopController) DeleteObject(context.Context, string) error     { return nil }
func (n *NoopController) IsObjectNotFoundErr(error) bool                 { return false }
func (n *NoopController) IsRetryableErr(error) bool                      { return false }
func (n *NoopController) Stop()                                          {}
func (n *NoopController) Wrap(c client.ObjectClient) client.ObjectClient { return c }
func (n *NoopController) WithRetrier(r Retrier) Controller {
	n.retrier = r
	return n
}
func (n *NoopController) WithHedger(h Hedger) Controller {
	n.hedger = h
	return n
}
func (n *NoopController) WithMetrics(m *Metrics) Controller {
	n.metrics = m
	return n
}
