package logs

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics instruments the logs section.
type Metrics struct {
	encodeSeconds prometheus.Histogram
	appendsTotal  prometheus.Counter
	recordCount   prometheus.Gauge
}

// NewMetrics creates a new set of metrics for the logs section.
func NewMetrics() *Metrics {
	return &Metrics{
		encodeSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "logs",
			Name:      "encode_seconds",

			Help: "Time taken encoding the logs section in seconds.",

			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),

		appendsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki_dataobj",
			Subsystem: "logs",
			Name:      "appends_total",

			Help: "The total number of log records appended.",
		}),

		recordCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_dataobj",
			Subsystem: "logs",
			Name:      "records_count",

			Help: "The current number of log records buffered; this resets after an encode.",
		}),
	}
}

// Register registers metrics to report to reg.
func (m *Metrics) Register(reg prometheus.Registerer) error {
	var errs []error
	errs = append(errs, reg.Register(m.encodeSeconds))
	errs = append(errs, reg.Register(m.appendsTotal))
	errs = append(errs, reg.Register(m.recordCount))
	return errors.Join(errs...)
}

// Unregister unregisters metrics from the provided Registerer.
func (m *Metrics) Unregister(reg prometheus.Registerer) {
	reg.Unregister(m.encodeSeconds)
	reg.Unregister(m.appendsTotal)
	reg.Unregister(m.recordCount)
}
