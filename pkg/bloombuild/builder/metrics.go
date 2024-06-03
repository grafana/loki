package builder

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	metricsNamespace = "loki"
	metricsSubsystem = "bloombuilder"

	statusSuccess = "success"
	statusFailure = "failure"
)

type Metrics struct {
	running prometheus.Gauge

	taskStarted   prometheus.Counter
	taskCompleted *prometheus.CounterVec
	taskDuration  *prometheus.HistogramVec
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		running: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "running",
			Help:      "Value will be 1 if the bloom builder is currently running on this instance",
		}),

		taskStarted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "task_started_total",
			Help:      "Total number of task started",
		}),
		taskCompleted: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "task_completed_total",
			Help:      "Total number of task completed",
		}, []string{"status"}),
		taskDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "task_duration_seconds",
			Help:      "Time spent processing a task.",
			// Buckets in seconds:
			Buckets: append(
				// 1s --> 1h (steps of 10 minutes)
				prometheus.LinearBuckets(1, 600, 6),
				// 1h --> 24h (steps of 1 hour)
				prometheus.LinearBuckets(3600, 3600, 24)...,
			),
		}, []string{"status"}),
	}
}
