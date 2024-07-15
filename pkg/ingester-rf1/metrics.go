package ingesterrf1

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type ingesterMetrics struct {
	autoForgetUnhealthyIngestersTotal prometheus.Counter
	flushDuration                     prometheus.Histogram
	flushFailuresTotal                prometheus.Counter
	flushesTotal                      prometheus.Counter
	flushQueues                       prometheus.Gauge
	flushSize                         prometheus.Histogram
	limiterEnabled                    prometheus.Gauge
	segmentSize                       prometheus.Histogram
	// Shutdown marker for ingester scale down.
	shutdownMarker prometheus.Gauge
}

func newIngesterMetrics(r prometheus.Registerer, metricsNamespace string) *ingesterMetrics {
	return &ingesterMetrics{
		autoForgetUnhealthyIngestersTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_rf1_autoforget_unhealthy_ingesters_total",
			Help: "Total number of ingesters automatically forgotten",
		}),
		flushDuration: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_rf1_flush_duration_seconds",
			Help:                        "The flush duration (in seconds)",
			Buckets:                     prometheus.ExponentialBuckets(0.001, 4, 8),
			NativeHistogramBucketFactor: 1.1,
		}),
		flushFailuresTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_rf1_flush_failures_total",
			Help: "The total number of failed flushes.",
		}),
		flushesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_rf1_flushes_total",
			Help: "The total number of flushes.",
		}),
		flushQueues: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_rf1_flush_queues",
			Help: "The total number of flush queues.",
		}),
		flushSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_rf1_flush_size_bytes",
			Help:                        "The flush size (as written to object storage).",
			Buckets:                     prometheus.ExponentialBuckets(100, 10, 8),
			NativeHistogramBucketFactor: 1.1,
		}),
		limiterEnabled: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_rf1_limiter_enabled",
			Help: "1 if the limiter is enabled, otherwise 0.",
		}),
		segmentSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_rf1_segment_size_bytes",
			Help:                        "The segment size (uncompressed in memory).",
			Buckets:                     prometheus.ExponentialBuckets(100, 10, 8),
			NativeHistogramBucketFactor: 1.1,
		}),
		shutdownMarker: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_rf1_shutdown_marker",
			Help: "1 if prepare shutdown has been called, 0 otherwise",
		}),
	}
}
