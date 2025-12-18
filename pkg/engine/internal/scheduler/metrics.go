package scheduler

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// metrics is a container of metrics for a scheduler.
type metrics struct {
	// registry to collect metrics as a unit.
	reg *prometheus.Registry

	tasksTotal    *prometheus.CounterVec
	streamsTotal  *prometheus.CounterVec
	connsTotal    prometheus.Counter
	backoffsTotal prometheus.Counter

	taskQueueSeconds prometheus.Histogram
	taskExecSeconds  prometheus.Histogram
}

func newMetrics() *metrics {
	reg := prometheus.NewRegistry()

	return &metrics{
		reg: reg,

		tasksTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_tasks_total",
			Help: "Total number of tasks by state, counting transitions into state",
		}, []string{"state"}),
		streamsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_streams_total",
			Help: "Total number of streams by state, counting transitions into state",
		}, []string{"state"}),
		connsTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_connections_total",
			Help: "Total number of connections to the scheduler for any purpose (control or data plane)",
		}),
		backoffsTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_assignment_backoffs_total",
			Help: "Total number of times the scheduler has backed off of assigning tasks to a worker because of HTTP 429 errors",
		}),

		taskQueueSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_task_queue_seconds",
			Help: "Number of seconds a task sat in a queue before being assigned to a worker thread",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),

		taskExecSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_task_exec_seconds",
			Help: "Number of seconds a task took to complete successfully",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
	}
}

// Register registers metrics to report to reg.
func (m *metrics) Register(reg prometheus.Registerer) error { return reg.Register(m.reg) }

// Unregister unregisters metrics from the provided Registerer.
func (m *metrics) Unregister(reg prometheus.Registerer) { reg.Unregister(m.reg) }
