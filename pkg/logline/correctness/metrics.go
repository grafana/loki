package correctness

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds Prometheus metrics for correctness verification.
type Metrics struct {
	cyclesTotal          prometheus.Counter
	cycleDuration        prometheus.Histogram
	cycleErrorsTotal     prometheus.Counter
	cycleSkippedTotal    *prometheus.CounterVec
	totalTests           prometheus.Counter
	totalCorrectTests    prometheus.Counter
	totalIncorrectTests  prometheus.Counter
	falsePositivesTotal  prometheus.Counter
	falseNegativesTotal  prometheus.Counter
	lastSuccessfulTestTs prometheus.Gauge
	lastIncorrectTestTs  prometheus.Gauge
}

// NewMetrics creates a new metrics registry for correctness verification.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		cyclesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_correctness_cycles_total",
			Help: "Total number of correctness cycles executed.",
		}),
		cycleDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_index_correctness_cycle_duration_seconds",
			Help:    "Duration of correctness cycles in seconds.",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 20, 30, 60, 120, 180, 300},
		}),
		cycleErrorsTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_correctness_cycle_errors_total",
			Help: "Total number of cycle-level failures.",
		}),
		cycleSkippedTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "logline_index_correctness_cycles_skipped_total",
			Help: "Total number of skipped cycles by reason.",
		}, []string{"reason"}),
		totalTests: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_correctness_tests_total",
			Help: "Total completed correctness tests.",
		}),
		totalCorrectTests: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_correctness_tests_correct_total",
			Help: "Total completed tests that passed correctness checks.",
		}),
		totalIncorrectTests: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_correctness_tests_incorrect_total",
			Help: "Total completed tests that failed correctness checks.",
		}),
		falsePositivesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_correctness_false_positives_total",
			Help: "Total false-positive document ranges reported by the index.",
		}),
		falseNegativesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_correctness_false_negatives_total",
			Help: "Total tests where a known Loki match was not covered by index results.",
		}),
		lastSuccessfulTestTs: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_index_correctness_last_successful_test_timestamp_seconds",
			Help: "Unix timestamp of the last successful correctness test.",
		}),
		lastIncorrectTestTs: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_index_correctness_last_incorrect_test_timestamp_seconds",
			Help: "Unix timestamp of the last failed correctness test.",
		}),
	}
}
