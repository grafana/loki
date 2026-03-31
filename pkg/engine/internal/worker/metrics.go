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

	// Per-pass phase histograms: one observation per drain-loop iteration.
	passReadSeconds    prometheus.Histogram
	passComputeSeconds prometheus.Histogram
	passWriteSeconds   prometheus.Histogram

	// Per-task phase histograms: one observation per completed task (accumulated time).
	taskReadSeconds    prometheus.Histogram
	taskComputeSeconds prometheus.Histogram
	taskWriteSeconds   prometheus.Histogram
}

func newMetrics() *metrics {
	reg := prometheus.NewRegistry()

	nativeHistOpts := func(name, help string) prometheus.HistogramOpts {
		return prometheus.HistogramOpts{
			Name: name,
			Help: help,

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}
	}

	return &metrics{
		reg: reg,

		tasksAssignedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_worker_tasks_assigned_total",
			Help: "Total number of tasks assigned to the worker",
		}),
		taskExecSeconds: promauto.With(reg).NewHistogram(nativeHistOpts(
			"loki_engine_worker_task_exec_seconds",
			"Number of seconds a task took to complete successfully",
		)),

		// Per-pass phase histograms.
		passReadSeconds: promauto.With(reg).NewHistogram(nativeHistOpts(
			"loki_engine_worker_pass_read_seconds",
			"Time in seconds for one read phase iteration (one pipeline.Read call)",
		)),
		passComputeSeconds: promauto.With(reg).NewHistogram(nativeHistOpts(
			"loki_engine_worker_pass_compute_seconds",
			"Time in seconds for one compute phase iteration (processing between read and write)",
		)),
		passWriteSeconds: promauto.With(reg).NewHistogram(nativeHistOpts(
			"loki_engine_worker_pass_write_seconds",
			"Time in seconds for one write phase iteration (sending records to sinks)",
		)),

		// Per-task aggregate phase histograms.
		taskReadSeconds: promauto.With(reg).NewHistogram(nativeHistOpts(
			"loki_engine_worker_task_read_seconds",
			"Total read phase time in seconds accumulated across all passes for one task",
		)),
		taskComputeSeconds: promauto.With(reg).NewHistogram(nativeHistOpts(
			"loki_engine_worker_task_compute_seconds",
			"Total compute phase time in seconds accumulated across all passes for one task",
		)),
		taskWriteSeconds: promauto.With(reg).NewHistogram(nativeHistOpts(
			"loki_engine_worker_task_write_seconds",
			"Total write phase time in seconds accumulated across all passes for one task",
		)),
	}
}

// Register registers metrics to report to reg.
func (m *metrics) Register(reg prometheus.Registerer) error { return reg.Register(m.reg) }

// Unregister unregisters metrics from the provided Registerer.
func (m *metrics) Unregister(reg prometheus.Registerer) { reg.Unregister(m.reg) }
