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
			Name:    "loki_ingester_rf1_flush_duration",
			Help:    "The flush duration (in seconds)",
			Buckets: []float64{0.1, 0.2, 0.5, 1, 2.5, 5, 7.5, 10},
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
			Name:    "loki_ingester_rf1_flush_size_megabytes",
			Help:    "The flush size (as written to object storage).",
			Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 7.5, 10},
		}),
		limiterEnabled: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_rf1_limiter_enabled",
			Help: "1 if the limiter is enabled, otherwise 0.",
		}),
		segmentSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_ingester_rf1_segment_size_megabytes",
			Help:    "The segment size (uncompressed in memory).",
			Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 7.5, 10},
		}),
		shutdownMarker: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_rf1_shutdown_marker",
			Help: "1 if prepare shutdown has been called, 0 otherwise",
		}),
	}
}
