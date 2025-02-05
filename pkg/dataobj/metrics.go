package dataobj

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/streams"
)

// metrics provides instrumnetation for the dataobj package.
type metrics struct {
	logs     *logs.Metrics
	streams  *streams.Metrics
	encoding *encoding.Metrics

	targetPageSize   prometheus.Gauge
	targetObjectSize prometheus.Gauge

	appendTime    prometheus.Histogram
	buildTime     prometheus.Histogram
	flushFailures prometheus.Counter

	sizeEstimate prometheus.Gauge
	builtSize    prometheus.Histogram
}

// newMetrics creates a new set of [metrics] for instrumenting data objects.
func newMetrics() *metrics {
	return &metrics{
		logs:     logs.NewMetrics(),
		streams:  streams.NewMetrics(),
		encoding: encoding.NewMetrics(),
		targetPageSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "config_target_page_size_bytes",

			Help: "Configured target page size in bytes.",
		}),

		targetObjectSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "config_target_object_size_bytes",

			Help: "Configured target object size in bytes.",
		}),

		appendTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "append_time_seconds",

			Help: "Time taken appending a set of log lines in a stream to a data object.",

			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),

		buildTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "build_time_seconds",

			Help: "Time taken building a data object to flush.",

			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),

		sizeEstimate: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "size_estimate_bytes",

			Help: "Current estimated size of the data object in bytes.",
		}),

		builtSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "built_size_bytes",

			Help: "Distribution of constructed data object sizes in bytes.",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),

		flushFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "flush_failures_total",

			Help: "Total number of flush failures.",
		}),
	}
}

// ObserveConfig updates config metrics based on the provided [BuilderConfig].
func (m *metrics) ObserveConfig(cfg BuilderConfig) {
	m.targetPageSize.Set(float64(cfg.TargetPageSize))
	m.targetObjectSize.Set(float64(cfg.TargetObjectSize))
}

// Register registers metrics to report to reg.
func (m *metrics) Register(reg prometheus.Registerer) error {
	var errs []error

	errs = append(errs, m.logs.Register(reg))
	errs = append(errs, m.streams.Register(reg))
	errs = append(errs, m.encoding.Register(reg))

	errs = append(errs, reg.Register(m.targetPageSize))
	errs = append(errs, reg.Register(m.targetObjectSize))

	errs = append(errs, reg.Register(m.appendTime))
	errs = append(errs, reg.Register(m.buildTime))

	errs = append(errs, reg.Register(m.sizeEstimate))
	errs = append(errs, reg.Register(m.builtSize))
	errs = append(errs, reg.Register(m.flushFailures))

	return errors.Join(errs...)
}

// Unregister unregisters metrics from the provided Registerer.
func (m *metrics) Unregister(reg prometheus.Registerer) {
	m.logs.Unregister(reg)
	m.streams.Unregister(reg)
	m.encoding.Unregister(reg)

	reg.Unregister(m.targetPageSize)
	reg.Unregister(m.targetObjectSize)

	reg.Unregister(m.appendTime)
	reg.Unregister(m.buildTime)

	reg.Unregister(m.sizeEstimate)
	reg.Unregister(m.builtSize)
	reg.Unregister(m.flushFailures)
}
