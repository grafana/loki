package pattern

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type ingesterMetrics struct {
	flushQueueLength       prometheus.Gauge
	patternsDiscardedTotal prometheus.Counter
	patternsDetectedTotal  prometheus.Counter
}

func newIngesterMetrics(r prometheus.Registerer, metricsNamespace string) *ingesterMetrics {
	return &ingesterMetrics{
		flushQueueLength: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "flush_queue_length",
			Help:      "The total number of series pending in the flush queue.",
		}),
		patternsDiscardedTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "patterns_evicted_total",
			Help:      "The total number of patterns evicted from the LRU cache.",
		}),
		patternsDetectedTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "patterns_detected_total",
			Help:      "The total number of patterns detected from incoming log lines.",
		}),
	}
}
