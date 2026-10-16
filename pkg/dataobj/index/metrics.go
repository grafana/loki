package index

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type CalculatorMetrics struct {
	calculationStepDuration *prometheus.HistogramVec
}

func NewCalculatorMetrics(reg prometheus.Registerer) *CalculatorMetrics {
	return &CalculatorMetrics{
		calculationStepDuration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_index_calculator_step_duration_seconds",
			Help:    "Time spent in each index calculation step (ProcessBatch + Flush) per logs section.",
			Buckets: prometheus.DefBuckets,
		}, []string{"step"}),
	}
}

func (m *CalculatorMetrics) observeStepDuration(step string, duration time.Duration) {
	m.calculationStepDuration.WithLabelValues(step).Observe(duration.Seconds())
}

// Outcomes reported by the result label of the index duration metric.
const (
	resultOK        = "ok"
	resultError     = "error"
	resultCancelled = "cancelled"
)

// IndexerMetrics holds every metric a [SimpleIndexer] reports.
type IndexerMetrics struct {
	duration        *prometheus.HistogramVec
	releaseFailures prometheus.Counter
}

// NewIndexerMetrics creates the metrics for a [SimpleIndexer] and registers
// them with reg.
func NewIndexerMetrics(reg prometheus.Registerer) *IndexerMetrics {
	factory := promauto.With(reg)

	duration := factory.NewHistogramVec(prometheus.HistogramOpts{
		Name: "loki_dataobj_builder_index_duration_seconds",
		Help: "Time taken to build and upload the index for a single data object.",

		Buckets:                         prometheus.DefBuckets,
		NativeHistogramBucketFactor:     1.1,
		NativeHistogramMaxBucketNumber:  100,
		NativeHistogramMinResetDuration: 0,
	}, []string{"result"})

	// Report both outcomes from the start, so that a rate over failures reads
	// as zero rather than going missing until the first one happens.
	duration.WithLabelValues(resultOK)
	duration.WithLabelValues(resultError)
	duration.WithLabelValues(resultCancelled)

	return &IndexerMetrics{
		duration: duration,
		releaseFailures: factory.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_builder_index_release_failures_total",
			Help: "Total number of failures to release an index object's scratch storage.",
		}),
	}
}

// observeIndex records how long an attempt to index a data object took, and
// whether it succeeded.
func (m *IndexerMetrics) observeIndex(duration time.Duration, err error) {
	var result string
	switch {
	case err == nil:
		result = resultOK
	case errors.Is(err, context.Canceled):
		result = resultCancelled
	default:
		result = resultError
	}

	m.duration.WithLabelValues(result).Observe(duration.Seconds())
}
