package worker

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Task type label values used to partition per-task worker metrics. A leaf task
// reads directly from storage (it has no external task sources); a non-leaf
// task receives its input from one or more child tasks.
const (
	taskTypeLeaf    = "leaf"
	taskTypeNonLeaf = "non_leaf"
)

// metrics is a container of metrics for a worker.
type metrics struct {
	// registry to collect metrics as a unit.
	reg *prometheus.Registry

	tasksAssignedTotal       prometheus.Counter
	rejectedAssignmentsTotal prometheus.Counter
	taskExecSeconds          *prometheus.HistogramVec

	// Per-pass phase durations (one observation per loop iteration).
	passReadSeconds prometheus.Histogram
	passSendSeconds prometheus.Histogram

	// Task-wide phase durations (one observation per drainPipeline call),
	// partitioned by task_type.
	taskOpenSeconds *prometheus.HistogramVec
	taskReadSeconds *prometheus.HistogramVec
	taskSendSeconds *prometheus.HistogramVec

	// setupSeconds measures the time spent preparing a task for execution
	// (before draining its pipeline), partitioned by task_type.
	setupSeconds *prometheus.HistogramVec

	// Task status-update send path (worker -> scheduler).
	statusUpdateSeconds     prometheus.Histogram
	statusUpdateErrorsTotal *prometheus.CounterVec

	// Task I/O counters, read from per-task captures at task completion. Only
	// leaf tasks download from object storage today, so these are zero for
	// non-leaf tasks until that changes.
	pagesDownloadedTotal prometheus.Counter
	pagesPrunedTotal     prometheus.Counter
	bytesDownloadedTotal prometheus.Counter

	// Per-operator-type cost; operator_type is bounded (operator/parser names).
	operatorSelfSeconds  *prometheus.HistogramVec
	operatorRowsInTotal  *prometheus.CounterVec
	operatorRowsOutTotal *prometheus.CounterVec
}

func newMetrics() *metrics {
	reg := prometheus.NewRegistry()

	return &metrics{
		reg: reg,

		tasksAssignedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_worker_tasks_assigned_total",
			Help: "Total number of tasks assigned to the worker",
		}),
		rejectedAssignmentsTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_worker_rejected_assignments_total",
			Help: "Total number of task assignments the worker rejected because no thread slot was available (worker-side counterpart to the scheduler's assignment_backoffs_total)",
		}),
		taskExecSeconds: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name: "loki_engine_worker_task_exec_seconds",
			Help: "Number of seconds a task took to complete successfully",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"task_type"}),

		passReadSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_worker_pass_read_seconds",
			Help: "Duration of a single read-phase pass",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
		passSendSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_worker_pass_send_seconds",
			Help: "Duration of a single send-phase pass",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),

		taskOpenSeconds: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name: "loki_engine_worker_task_open_seconds",
			Help: "Total time spent opening a task's pipeline (Pipeline.Open)",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"task_type"}),
		taskReadSeconds: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name: "loki_engine_worker_task_read_seconds",
			Help: "Total time spent in the read phase (Pipeline.Read) for a task",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"task_type"}),
		taskSendSeconds: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name: "loki_engine_worker_task_send_seconds",
			Help: "Total time spent in the send phase for a task",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"task_type"}),

		setupSeconds: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name: "loki_engine_worker_setup_seconds",
			Help: "Time spent preparing a task for execution before its pipeline is drained",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"task_type"}),

		statusUpdateSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_worker_status_update_seconds",
			Help: "Time spent sending a task's terminal status update to the scheduler and waiting for acknowledgement",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
		statusUpdateErrorsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_worker_status_update_errors_total",
			Help: "Total number of failures sending a task's terminal status update to the scheduler, by error class",
		}, []string{"error_class"}),

		pagesDownloadedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_worker_pages_downloaded_total",
			Help: "Total number of pages downloaded from object storage during task execution",
		}),
		pagesPrunedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_worker_pages_pruned_total",
			Help: "Total number of pages pruned via metadata before download during task execution",
		}),
		bytesDownloadedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_worker_bytes_downloaded_total",
			Help: "Total number of bytes downloaded from object storage during task execution",
		}),

		operatorSelfSeconds: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name: "loki_engine_worker_operator_self_seconds",
			Help: "Per-operator wall-clock (not CPU) self-time, exclusive of child operators, by operator type",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"operator_type"}),
		operatorRowsInTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_worker_operator_rows_in_total",
			Help: "Total rows an operator consumed from its child operators, by operator type (zero for leaves)",
		}, []string{"operator_type"}),
		operatorRowsOutTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_worker_operator_rows_out_total",
			Help: "Total rows produced by an operator, by operator type",
		}, []string{"operator_type"}),
	}
}

// Register registers metrics to report to reg.
func (m *metrics) Register(reg prometheus.Registerer) error { return reg.Register(m.reg) }

// Unregister unregisters metrics from the provided Registerer.
func (m *metrics) Unregister(reg prometheus.Registerer) { reg.Unregister(m.reg) }

// observeOperatorCost records each operator's self-time and row counts, by type.
func (m *metrics) observeOperatorCost(nodes []pipelineNode) {
	for _, n := range nodes {
		m.operatorSelfSeconds.WithLabelValues(n.OpType).Observe(n.SelfDuration.Seconds())
		m.operatorRowsInTotal.WithLabelValues(n.OpType).Add(float64(n.RowsIn))
		m.operatorRowsOutTotal.WithLabelValues(n.OpType).Add(float64(n.RowsOut))
	}
}
