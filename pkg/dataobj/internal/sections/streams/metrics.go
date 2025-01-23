package streams

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics instruments the streams section.
type Metrics struct {
	encodeSeconds prometheus.Histogram
	recordsTotal  prometheus.Counter
	streamCount   prometheus.Gauge
	minTimestamp  prometheus.Gauge
	maxTimestamp  prometheus.Gauge
}

// NewMetrics creates a new set of metrics for the streams section.
func NewMetrics() *Metrics {
	return &Metrics{
		encodeSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "streams",
			Name:      "encode_seconds",

			Help: "Time taken encoding streams section in seconds.",

			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),

		recordsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki_dataobj",
			Subsystem: "streams",
			Name:      "records_total",

			Help: "The total number of stream records appended.",
		}),

		streamCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_dataobj",
			Subsystem: "streams",
			Name:      "stream_count",

			Help: "The current number of tracked streams; this resets after an encode.",
		}),

		minTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_dataobj",
			Subsystem: "streams",
			Name:      "min_timestamp",

			Help: "The minimum timestamp (in unix seconds) across all streams; this resets after an encode.",
		}),

		maxTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_dataobj",
			Subsystem: "streams",
			Name:      "max_timestamp",

			Help: "The maximum timestamp (in unix seconds) across all streams; this resets after an encode.",
		}),
	}
}

// Register registers metrics to report to reg.
func (m *Metrics) Register(reg prometheus.Registerer) error {
	var errs []error
	errs = append(errs, reg.Register(m.encodeSeconds))
	errs = append(errs, reg.Register(m.recordsTotal))
	errs = append(errs, reg.Register(m.streamCount))
	errs = append(errs, reg.Register(m.minTimestamp))
	errs = append(errs, reg.Register(m.maxTimestamp))
	return errors.Join(errs...)
}

// Unregister unregisters metrics from the provided Registerer.
func (m *Metrics) Unregister(reg prometheus.Registerer) {
	reg.Unregister(m.encodeSeconds)
	reg.Unregister(m.recordsTotal)
	reg.Unregister(m.streamCount)
	reg.Unregister(m.minTimestamp)
	reg.Unregister(m.maxTimestamp)
}
