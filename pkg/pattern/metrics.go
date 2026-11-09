package pattern

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type ingesterMetrics struct {
	flushQueueLength       prometheus.Gauge
	patternsDiscardedTotal *prometheus.CounterVec
	patternsDetectedTotal  *prometheus.CounterVec
	linesSkipped           *prometheus.CounterVec
	tokensPerLine          *prometheus.HistogramVec
	statePerLine           *prometheus.HistogramVec
	metricSamples          *prometheus.CounterVec
	kafkaRecordsTotal      prometheus.Counter
	kafkaRecordsFailed     prometheus.Counter
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
			Help:      "The total number of patterns evicted from the LRU cache.",
		}, []string{"tenant", "format", "pruned"}),
		patternsDetectedTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "patterns_detected_total",
			Help:      "The total number of patterns detected from incoming log lines.",
		}, []string{"tenant", "format"}),
		linesSkipped: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "patterns_dropped_total",
			Help:      "The total number of log lines skipped for pattern recognition.",
		}, []string{"tenant", "reason"}),
		tokensPerLine: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "tokens_per_line",
			Help:      "The number of tokens an incoming logline is split into for pattern recognition.",
			Buckets:   []float64{20, 40, 80, 120, 160, 320, 640, 1280},
		}, []string{"tenant", "format"}),
		statePerLine: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "state_per_line",
			Help:      "The number of items of additional state returned alongside tokens for pattern recognition.",
			Buckets:   []float64{20, 40, 80, 120, 160, 320, 640, 1280},
		}, []string{"tenant", "format"}),
		metricSamples: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "metric_samples",
			Help:      "The total number of metric samples created to write back to Loki.",
		}, []string{"tenant"}),
		kafkaRecordsTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_pattern_ingester_kafka_records_total",
			Help: "The total number of records consumed from Kafka.",
		}),
		kafkaRecordsFailed: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_pattern_ingester_kafka_records_failed_total",
			Help: "The total number of records consumed from Kafka that failed to be decoded or pushed.",
		}),
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
