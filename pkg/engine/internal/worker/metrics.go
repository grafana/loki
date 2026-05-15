package worker

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// metrics is a container of metrics for a worker.
type metrics struct {
	// registry to collect metrics as a unit.
	reg *prometheus.Registry

	tasksAssignedTotal prometheus.Counter
	taskExecSeconds    prometheus.Histogram

	// Per-pass phase durations (one observation per loop iteration).
	passComputeSeconds prometheus.Histogram
	passWriteSeconds   prometheus.Histogram

	// Task-wide phase durations (one observation per drainPipeline call).
	taskReadSeconds    prometheus.Histogram
	taskComputeSeconds prometheus.Histogram
	taskWriteSeconds   prometheus.Histogram
}

func newMetrics() *metrics {
	reg := prometheus.NewRegistry()

	return &metrics{
		reg: reg,

		tasksAssignedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_worker_tasks_assigned_total",
			Help: "Total number of tasks assigned to the worker",
		}),
		taskExecSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_worker_task_exec_seconds",
			Help: "Number of seconds a task took to complete successfully",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),

		passComputeSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_worker_pass_compute_seconds",
			Help: "Duration of a single compute phase pass",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
		passWriteSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_worker_pass_write_seconds",
			Help: "Duration of a single write phase pass",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),

		taskReadSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_worker_task_read_seconds",
			Help: "Total time spent in the read phase for a task",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
		taskComputeSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_worker_task_compute_seconds",
			Help: "Total time spent in the compute phase for a task",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
		taskWriteSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_worker_task_write_seconds",
			Help: "Total time spent in the write phase for a task",

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
