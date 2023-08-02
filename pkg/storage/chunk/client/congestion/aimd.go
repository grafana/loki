package congestion

import (
	"context"
	"io"
	"math"
	"time"

	"golang.org/x/time/rate"

	"github.com/grafana/loki/pkg/storage/chunk/client"
)

const (
	// start from 100 requests per second, increment on each successful response
	initialSize rate.Limit = 100
	// halve the rate on each failure
	backoffFactor = 0.5
	// set an upper bound (we've observed up to 7000 RPS on individual queries in our largest cells)
	upperBound = 10000
)

// AIMDStrategy implements the Additive-Increase/Multiplicative-Decrease algorithm which is used in TCP congestion avoidance.
// https://en.wikipedia.org/wiki/Additive_increase/multiplicative_decrease
type AIMDStrategy struct {
	inner client.ObjectClient

	retry RetryStrategy
	hedge HedgeStrategy

	limiter       *rate.Limiter
	backoffFactor float64
	upperBound    rate.Limit
}

func (a *AIMDStrategy) Wrap(client client.ObjectClient) client.ObjectClient {
	a.inner = client
	return a
}

func (a *AIMDStrategy) RetryStrategy() RetryStrategy {
	if a.retry == nil {
		return NoopRetryStrategy{}
	}

	return a.retry
}

func (a *AIMDStrategy) HedgeStrategy() HedgeStrategy {
	if a.hedge == nil {
		return NoopHedgeStrategy{}
	}

	return a.hedge
}

func NewAIMDStrategy(retry RetryStrategy, hedge HedgeStrategy) *AIMDStrategy {
	s := &AIMDStrategy{
		retry: retry,
		hedge: hedge,

		limiter:       rate.NewLimiter(initialSize, int(initialSize)),
		backoffFactor: backoffFactor,
		upperBound:    upperBound,
	}

	//metrics.currentLimit.Set(float64(s.limiter.Limit()))

	return s
}

func (a *AIMDStrategy) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return a.inner.PutObject(ctx, objectKey, object)
}

func (a *AIMDStrategy) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	// Only GetObject implements congestion avoidance; the other methods are either non-idempotent which means they
	// cannot be retried, or are too low volume to care

	rc, sz, err := a.retry.Do(
		func(attempt int) (io.ReadCloser, int64, error) {
			// in retry
			if attempt > 0 {
				// TODO(dannyk): define SetHTTPClient on client.ObjectClient interface
				// then use NoopHedgeStrategy to avoid hedging retries
			}

			// apply back-pressure while rate-limit has been exceeded
			for {
				// using Reserve() is slower because it assumes a constant wait time as tokens are replenished, but in experimentation
				// it's faster to sit in a hot loop and probe every so often if there are tokens available
				if !a.limiter.Allow() {
					time.Sleep(time.Millisecond * 10)
					continue
				}
				break
			}

			// it is vitally important that retries are DISABLED in the inner implementation.
			return a.inner.GetObject(ctx, objectKey)
		},
		a.inner.IsRetryableErr,
		a.additiveIncrease,
		a.multiplicativeDecrease,
	)

	//a.metrics.currentLimit.Set(float64(a.limiter.Limit()))

	return rc, sz, err
}

func (a *AIMDStrategy) List(ctx context.Context, prefix string, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	return a.inner.List(ctx, prefix, delimiter)
}

func (a *AIMDStrategy) DeleteObject(ctx context.Context, objectKey string) error {
	return a.inner.DeleteObject(ctx, objectKey)
}

func (a *AIMDStrategy) IsObjectNotFoundErr(err error) bool {
	return a.inner.IsObjectNotFoundErr(err)
}

func (a *AIMDStrategy) IsRetryableErr(err error) bool {
	return a.inner.IsRetryableErr(err)
}

func (a *AIMDStrategy) Stop() {
	a.inner.Stop()
}

// additiveIncrease increases the number of requests per second that can be sent linearly.
// it should never exceed the defined upper bound.
func (a *AIMDStrategy) additiveIncrease() {
	newLimit := a.limiter.Limit() + 1

	if newLimit > a.upperBound {
		newLimit = a.upperBound
	}

	a.limiter.SetLimit(newLimit)
	a.limiter.SetBurst(int(newLimit))
}

// multiplicativeDecrease reduces the number of requests per second that can be sent exponentially.
// it should never be set lower than 1.
func (a *AIMDStrategy) multiplicativeDecrease() {
	newLimit := math.Ceil(math.Max(1, float64(a.limiter.Limit())*a.backoffFactor))

	a.limiter.SetLimit(rate.Limit(newLimit))
	a.limiter.SetBurst(int(newLimit))
}

//
//type Metrics struct {
//	currentLimit prometheus.Gauge
//}
//
//func (m Metrics) Unregister() {
//	prometheus.Unregister(m.currentLimit)
//}
//
//func NewMetrics() Metrics {
//	m := Metrics{
//		currentLimit: prometheus.NewGauge(prometheus.GaugeOpts{
//			Namespace: "loki",
//			Subsystem: "store",
//			Name:      "congestion_control_limit",
//			Help:      "Current per-second request limit to control congestion",
//			ConstLabels: map[string]string{
//				"strategy": "aimd",
//			},
//		}),
//	}
//
//	prometheus.MustRegister(m.currentLimit)
//	return m
//}
