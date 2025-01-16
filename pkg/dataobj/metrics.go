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

	shaPrefixSize    prometheus.Metric
	targetPageSize   prometheus.Metric
	targetObjectSize prometheus.Metric

	appendTime prometheus.Histogram
	buildTime  prometheus.Histogram
	flushTime  prometheus.Histogram

	sizeEstimate prometheus.Gauge
}

// newMetrics creates a new set of [metrics] for instrumenting data objects.
func newMetrics(cfg BuilderConfig) *metrics {
	return &metrics{
		logs:     logs.NewMetrics(),
		streams:  streams.NewMetrics(),
		encoding: encoding.NewMetrics(),

		shaPrefixSize: prometheus.MustNewConstMetric(
			prometheus.NewDesc(
				"loki_dataobj_config_sha_prefix_size",
				"Configured SHA prefix size.",
				nil,
				nil,
			),
			prometheus.UntypedValue,
			float64(cfg.SHAPrefixSize),
		),

		targetPageSize: prometheus.MustNewConstMetric(
			prometheus.NewDesc(
				"loki_dataobj_config_target_page_size_bytes",
				"Configured target page size in bytes.",
				nil,
				nil,
			),
			prometheus.UntypedValue,
			float64(cfg.TargetPageSize),
		),

		targetObjectSize: prometheus.MustNewConstMetric(
			prometheus.NewDesc(
				"loki_dataobj_config_target_object_size_bytes",
				"Configured target object size in bytes.",
				nil,
				nil,
			),
			prometheus.UntypedValue,
			float64(cfg.TargetObjectSize),
		),

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

		flushTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "flush_time_seconds",

			Help: "Time taken flushing data objects to object storage.",

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
	}
}

// Register registers metrics to report to reg.
func (m *metrics) Register(reg prometheus.Registerer) error {
	var errs []error

	errs = append(errs, m.logs.Register(reg))
	errs = append(errs, m.streams.Register(reg))
	errs = append(errs, m.encoding.Register(reg))

	errs = append(errs, reg.Register(m.appendTime))
	errs = append(errs, reg.Register(m.buildTime))
	errs = append(errs, reg.Register(m.flushTime))

	errs = append(errs, reg.Register(m.sizeEstimate))

	return errors.Join(errs...)
}

// Unregister unregisters metrics from the provided Registerer.
func (m *metrics) Unregister(reg prometheus.Registerer) {
	m.logs.Unregister(reg)
	m.streams.Unregister(reg)
	m.encoding.Unregister(reg)

	reg.Unregister(m.appendTime)
	reg.Unregister(m.buildTime)
	reg.Unregister(m.flushTime)

	reg.Unregister(m.sizeEstimate)
}
