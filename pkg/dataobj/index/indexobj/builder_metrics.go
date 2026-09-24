package indexobj

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// BuilderMetrics provides instrumentation for a [Builder].
type BuilderMetrics struct {
	pointers      *pointers.Metrics
	indexPointers *indexpointers.Metrics
	streams       *streams.Metrics
	postings      *postings.Metrics
	stats         *stats.Metrics
	dataobj       *dataobj.Metrics

	targetPageSize   prometheus.Gauge
	targetObjectSize prometheus.Gauge

	appendTime     prometheus.Histogram
	appendFailures prometheus.Counter
	appendsTotal   prometheus.Counter

	buildTime     prometheus.Histogram
	flushFailures prometheus.Counter
	flushTotal    prometheus.Counter

	sizeEstimate prometheus.Gauge
	builtSize    prometheus.Histogram
}

// NewBuilderMetrics creates a new set of [BuilderMetrics] for instrumenting
// index objects and registers them with reg. If reg is nil the metrics are
// created but not registered.
func NewBuilderMetrics(reg prometheus.Registerer) *BuilderMetrics {
	factory := promauto.With(reg)

	m := &BuilderMetrics{
		indexPointers: indexpointers.NewMetrics(),
		pointers:      pointers.NewMetrics(),
		streams:       streams.NewMetrics(),
		postings:      postings.NewMetrics(),
		stats:         stats.NewMetrics(),
		dataobj:       dataobj.NewMetrics(),
		targetPageSize: factory.NewGauge(prometheus.GaugeOpts{
			Name: "loki_indexobj_config_target_page_size_bytes",

			Help: "Configured target page size in bytes.",
		}),

		targetObjectSize: factory.NewGauge(prometheus.GaugeOpts{
			Name: "loki_indexobj_config_target_object_size_bytes",

			Help: "Configured target object size in bytes.",
		}),

		appendTime: factory.NewHistogram(prometheus.HistogramOpts{
			Name: "loki_indexobj_append_time_seconds",

			Help: "Time taken appending a set of log lines in a stream to a data object.",

			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),

		appendFailures: factory.NewCounter(prometheus.CounterOpts{
			Name: "loki_indexobj_append_failures_total",
			Help: "Total number of append failures",
		}),

		appendsTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "loki_indexobj_appends_total",
			Help: "Total number of appends",
		}),

		buildTime: factory.NewHistogram(prometheus.HistogramOpts{
			Name: "loki_indexobj_build_time_seconds",

			Help: "Time taken building a data object to flush.",

			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),

		sizeEstimate: factory.NewGauge(prometheus.GaugeOpts{
			Name: "loki_indexobj_size_estimate_bytes",

			Help: "Current estimated size of the data object in bytes.",
		}),

		builtSize: factory.NewHistogram(prometheus.HistogramOpts{
			Name: "loki_indexobj_built_size_bytes",

			Help: "Distribution of constructed data object sizes in bytes.",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),

		flushFailures: factory.NewCounter(prometheus.CounterOpts{
			Name: "loki_indexobj_flush_failures_total",

			Help: "Total number of flush failures.",
		}),

		flushTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "loki_indexobj_flush_total",

			Help: "Total number of flushes.",
		}),
	}

	if reg != nil {
		if err := errors.Join(
			m.indexPointers.Register(reg),
			m.pointers.Register(reg),
			m.streams.Register(reg),
			m.postings.Register(reg),
			m.stats.Register(reg),
			m.dataobj.Register(reg),
		); err != nil {
			panic(fmt.Errorf("registering index object section metrics: %w", err))
		}
	}

	return m
}

// ObserveConfig updates config metrics based on the provided [BuilderConfig].
func (m *BuilderMetrics) ObserveConfig(cfg logsobj.BuilderBaseConfig) {
	m.targetPageSize.Set(float64(cfg.TargetPageSize))
	m.targetObjectSize.Set(float64(cfg.TargetObjectSize))
}
