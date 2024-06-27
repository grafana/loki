package pattern

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type ingesterMetrics struct {
	flushQueueLength       prometheus.Gauge
	patternsDiscardedTotal *prometheus.CounterVec
	patternsDetectedTotal  *prometheus.CounterVec
	tokensPerLine          *prometheus.HistogramVec
	metadataPerLine        *prometheus.HistogramVec
}

func newIngesterMetrics(r prometheus.Registerer, metricsNamespace string) *ingesterMetrics {
	return &ingesterMetrics{
		flushQueueLength: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "flush_queue_length",
			Help:      "The total number of series pending in the flush queue.",
		}),
		patternsDiscardedTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "patterns_evicted_total",
			Help:      "The total number of patterns evicted from the LRU cache, by log format.",
		}, []string{"format"}),
		patternsDetectedTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "patterns_detected_total",
			Help:      "The total number of patterns detected from incoming log lines, by log format.",
		}, []string{"format"}),
		tokensPerLine: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "tokens_per_line",
			Help:      "The number of tokens an incoming logline is split into for pattern recognision",
			Buckets:   []float64{5, 10, 20, 40, 80, 120, 160, 240, 320, 640, 1280},
		}, []string{"format"}),
		metadataPerLine: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "metadata_per_line",
			Help:      "The number of items of additional metadata returned alongside tokens for pattern recognition",
			Buckets:   []float64{5, 10, 20, 40, 80, 100, 120, 140, 160, 240, 320, 640, 1280},
		}, []string{"format"}),
	}
}

type ingesterQuerierMetrics struct {
	patternsPrunedTotal   prometheus.Counter
	patternsRetainedTotal prometheus.Counter
}

func newIngesterQuerierMetrics(r prometheus.Registerer, metricsNamespace string) *ingesterQuerierMetrics {
	return &ingesterQuerierMetrics{
		patternsPrunedTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "query_pruned_total",
			Help:      "The total number of patterns removed at query time by the pruning Drain instance",
		}),
		patternsRetainedTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "query_retained_total",
			Help:      "The total number of patterns retained at query time by the pruning Drain instance",
		}),
	}
}
