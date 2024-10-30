package ingesterrf1

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/storage/wal"
)

type ingesterMetrics struct {
	autoForgetUnhealthyIngestersTotal prometheus.Counter
	limiterEnabled                    prometheus.Gauge
	// Shutdown marker for ingester scale down.
	shutdownMarker     prometheus.Gauge
	flushesTotal       prometheus.Counter
	flushFailuresTotal prometheus.Counter
	flushQueues        prometheus.Gauge
	flushDuration      prometheus.Histogram
	flushSize          prometheus.Histogram
	segmentMetrics     *wal.SegmentMetrics
}

func newIngesterMetrics(r prometheus.Registerer) *ingesterMetrics {
	return &ingesterMetrics{
		autoForgetUnhealthyIngestersTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_rf1_autoforget_unhealthy_ingesters_total",
			Help: "Total number of ingesters automatically forgotten.",
		}),
		limiterEnabled: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_rf1_limiter_enabled",
			Help: "1 if the limiter is enabled, otherwise 0.",
		}),
		shutdownMarker: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_rf1_shutdown_marker",
			Help: "1 if prepare shutdown has been called, 0 otherwise.",
		}),
		flushesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_rf1_flushes_total",
			Help: "The total number of flushes.",
		}),
		flushFailuresTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_rf1_flush_failures_total",
			Help: "The total number of failed flushes.",
		}),
		flushQueues: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_rf1_flush_queues",
			Help: "The total number of flush queues.",
		}),
		flushDuration: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_rf1_flush_duration_seconds",
			Help:                        "The flush duration (in seconds).",
			Buckets:                     prometheus.ExponentialBuckets(0.001, 4, 8),
			NativeHistogramBucketFactor: 1.1,
		}),
		flushSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_rf1_flush_size_bytes",
			Help:                        "The flush size (as written to object storage).",
			Buckets:                     prometheus.ExponentialBuckets(100, 10, 8),
			NativeHistogramBucketFactor: 1.1,
		}),
		segmentMetrics: wal.NewSegmentMetrics(r),
	}
}
