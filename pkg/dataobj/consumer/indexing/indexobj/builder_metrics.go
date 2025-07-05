package indexobj

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// builderMetrics provides instrumnetation for a [Builder].
type builderMetrics struct {
	pointers *pointers.Metrics
	streams  *streams.Metrics
	dataobj  *dataobj.Metrics

	targetPageSize   prometheus.Gauge
	targetObjectSize prometheus.Gauge

	appendTime    prometheus.Histogram
	buildTime     prometheus.Histogram
	flushFailures prometheus.Counter

	sizeEstimate prometheus.Gauge
	builtSize    prometheus.Histogram
}

// newBuilderMetrics creates a new set of [builderMetrics] for instrumenting
// logs objects.
func newBuilderMetrics() *builderMetrics {
	return &builderMetrics{
		pointers: pointers.NewMetrics(),
		streams:  streams.NewMetrics(),
		dataobj:  dataobj.NewMetrics(),
		targetPageSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Subsystem: "indexobj",
			Name:      "config_target_page_size_bytes",

			Help: "Configured target page size in bytes.",
		}),

		targetObjectSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Subsystem: "indexobj",
			Name:      "config_target_object_size_bytes",

			Help: "Configured target object size in bytes.",
		}),

		appendTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "indexobj",
			Name:      "_append_time_seconds",

			Help: "Time taken appending a set of log lines in a stream to a data object.",

			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),

		buildTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "indexobj",
			Name:      "build_time_seconds",

			Help: "Time taken building a data object to flush.",

			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),

		sizeEstimate: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Subsystem: "indexobj",
			Name:      "size_estimate_bytes",

			Help: "Current estimated size of the data object in bytes.",
		}),

		builtSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "indexobj",
			Name:      "built_size_bytes",

			Help: "Distribution of constructed data object sizes in bytes.",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),

		flushFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Subsystem: "indexobj",
			Name:      "flush_failures_total",

			Help: "Total number of flush failures.",
		}),
	}
}

// ObserveConfig updates config metrics based on the provided [BuilderConfig].
func (m *builderMetrics) ObserveConfig(cfg BuilderConfig) {
	m.targetPageSize.Set(float64(cfg.TargetPageSize))
	m.targetObjectSize.Set(float64(cfg.TargetObjectSize))
}

// Register registers metrics to report to reg.
func (m *builderMetrics) Register(reg prometheus.Registerer) error {
	var errs []error

	errs = append(errs, m.pointers.Register(reg))
	errs = append(errs, m.streams.Register(reg))
	errs = append(errs, m.dataobj.Register(reg))

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
func (m *builderMetrics) Unregister(reg prometheus.Registerer) {
	m.pointers.Unregister(reg)
	m.streams.Unregister(reg)
	m.dataobj.Unregister(reg)

	reg.Unregister(m.targetPageSize)
	reg.Unregister(m.targetObjectSize)

	reg.Unregister(m.appendTime)
	reg.Unregister(m.buildTime)

	reg.Unregister(m.sizeEstimate)
	reg.Unregister(m.builtSize)
	reg.Unregister(m.flushFailures)
}
