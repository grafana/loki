package scratch

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics is a set of metrics for monitoring a [Store].
type Metrics struct {
	handles prometheus.Gauge
	bytes   prometheus.Gauge

	requestsSeconds *prometheus.HistogramVec
}

// NewMetrics returns a new Metrics. Returned Metrics must be registered to a
// registerer using [Metrics.Register].
func NewMetrics() *Metrics {
	return &Metrics{
		handles: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_scratch_store_handles",
			Help: "Current number of handles in the scratch store",
		}),
		bytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_scratch_store_bytes",
			Help: "Current number of bytes stored in the scratch store",
		}),

		requestsSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "loki_scratch_store_requests_seconds",
			Help: "Time taken to perform operations on the scratch store",

			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{"operation", "result"}),
	}
}

// Register registers metrics to report to reg.
func (m *Metrics) Register(reg prometheus.Registerer) error {
	var errs []error

	errs = append(errs, reg.Register(m.handles))
	errs = append(errs, reg.Register(m.bytes))
	errs = append(errs, reg.Register(m.requestsSeconds))

	return errors.Join(errs...)
}

// Unregister unregisters metrics from reg.
func (m *Metrics) Unregister(reg prometheus.Registerer) {
	reg.Unregister(m.handles)
	reg.Unregister(m.bytes)
	reg.Unregister(m.requestsSeconds)
}

type observedStore struct {
	metrics *Metrics
	inner   Store

	handleSizes sync.Map // map[Handle]uint (uint is byte size of data in handle)
}

var _ Store = (*observedStore)(nil)

// ObserveStore wraps a store and accumulates statistics into the provided
// Metrics. If metrics is nil, ObserveStore performs no wrapping.
func ObserveStore(metrics *Metrics, store Store) Store {
	if metrics == nil {
		return store // nothing to do
	}

	return &observedStore{
		metrics: metrics,
		inner:   store,
	}
}

func (os *observedStore) Put(p []byte) Handle {
	start := time.Now()
	handle := os.inner.Put(p)
	duration := time.Since(start)

	os.handleSizes.Store(handle, uint(len(p)))

	os.metrics.handles.Add(1)
	os.metrics.bytes.Add(float64(len(p)))
	os.metrics.requestsSeconds.WithLabelValues("Put", "success").Observe(duration.Seconds())
	return handle
}

func (os *observedStore) Read(h Handle) (io.ReadSeekCloser, error) {
	start := time.Now()
	reader, err := os.inner.Read(h)
	duration := time.Since(start)

	os.metrics.requestsSeconds.WithLabelValues("Read", errorToResult(err)).Observe(duration.Seconds())

	return reader, err
}

func errorToResult(err error) string {
	if err == nil {
		return "success"
	}
	return "error"
}

func (os *observedStore) Remove(h Handle) error {
	start := time.Now()
	err := os.inner.Remove(h)
	duration := time.Since(start)

	if value, existed := os.handleSizes.LoadAndDelete(h); existed {
		os.metrics.handles.Sub(1)
		os.metrics.bytes.Sub(float64(value.(uint)))
	}

	os.metrics.requestsSeconds.WithLabelValues("Remove", errorToResult(err)).Observe(duration.Seconds())

	return err
}
