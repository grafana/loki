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
	taskTime      *prometheus.HistogramVec
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
		taskTime: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "task_time_seconds",
			Help:      "Time spent processing a task.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"status"}),
	}
}
