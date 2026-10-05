package correctness

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds Prometheus metrics for experimental chunk correctness.
type Metrics struct {
	cyclesTotal         prometheus.Counter
	cycleDuration       prometheus.Histogram
	cycleErrorsTotal    prometheus.Counter
	cycleSkippedTotal   *prometheus.CounterVec
	testsCorrectTotal   prometheus.Counter
	testsIncorrectTotal prometheus.Counter
	lastCorrectTestTs   prometheus.Gauge
	lastIncorrectTestTs prometheus.Gauge
}

// NewMetrics creates correctness metrics registered on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		cyclesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_exp_chunk_correctness_cycles_total",
			Help: "Total number of correctness cycles started.",
		}),
		cycleDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_exp_chunk_correctness_cycle_duration_seconds",
			Help:    "Duration of correctness cycles in seconds.",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 180, 300},
		}),
		cycleErrorsTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_exp_chunk_correctness_cycle_errors_total",
			Help: "Total number of cycle-level failures.",
		}),
		cycleSkippedTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_exp_chunk_correctness_cycles_skipped_total",
			Help: "Total number of skipped cycles by reason.",
		}, []string{"reason"}),
		testsCorrectTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_exp_chunk_correctness_tests_correct_total",
			Help: "Total samples whose timestamp, line, and stream labels were found on the experimental querier.",
		}),
		testsIncorrectTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_exp_chunk_correctness_tests_incorrect_total",
			Help: "Total samples missing from the experimental querier after the lag grace period.",
		}),
		lastCorrectTestTs: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_exp_chunk_correctness_last_correct_test_timestamp_seconds",
			Help: "Unix timestamp of the last correct test.",
		}),
		lastIncorrectTestTs: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_exp_chunk_correctness_last_incorrect_test_timestamp_seconds",
			Help: "Unix timestamp of the last incorrect test.",
		}),
	}
}
