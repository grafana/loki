package worker

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/engine/internal/metrictimer"
	"github.com/grafana/loki/v3/pkg/engine/internal/obslock"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

type taskType string

// Task type label values used to partition per-task worker metrics. A leaf task
// reads directly from storage (it has no external task sources); a non-leaf
// task receives its input from one or more child tasks.
const (
	taskTypeLeaf    taskType = "leaf"
	taskTypeNonLeaf taskType = "non_leaf"
)

func (t taskType) String() string { return string(t) }

// Handler phase label values.
const (
	phaseTotal           metrictimer.Phase = "total"
	phaseSourceWriteWait metrictimer.Phase = "source_write_wait"
)

type sendMode string

// send mode label values. The Prometheus label is named "mode".
const (
	sendModeSync     sendMode = "sync"
	sendModeAsync    sendMode = "async"
	sendModeInternal sendMode = "internal"
)

func (m sendMode) String() string { return string(m) }

// Slot phase label values.
const (
	slotPhaseCompute metrictimer.Phase = "compute"
	slotPhaseComm    metrictimer.Phase = "comm"
)

// Outcome label values shared by bounded worker metrics.
const (
	outcomeAck        metrictimer.Outcome = "ack"
	outcomeNack       metrictimer.Outcome = "nack"
	outcomeSuccess    metrictimer.Outcome = "success"
	outcomeCanceled   metrictimer.Outcome = "canceled"
	outcomeTimeout    metrictimer.Outcome = "timeout"
	outcomeConnClosed metrictimer.Outcome = "conn_closed"
	outcomeError      metrictimer.Outcome = "error"
	outcomeNack429    metrictimer.Outcome = "nack_429"

	outcomeAssigned   metrictimer.Outcome = "assigned"
	outcomeShutdown   metrictimer.Outcome = "shutdown"
	outcomeUnknown    metrictimer.Outcome = "unknown"
	outcomeFailed     metrictimer.Outcome = "failed"
	outcomeSent       metrictimer.Outcome = "sent"
	outcomeNoReady429 metrictimer.Outcome = "no_ready_429"
	outcomeCtxError   metrictimer.Outcome = "ctx_error"

	outcomeNone        metrictimer.Outcome = "none"
	outcomeRejected    metrictimer.Outcome = "rejected"
	outcomeServerError metrictimer.Outcome = "server_error"
	outcomeOther       metrictimer.Outcome = "other"
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

	// Terminal task-result send path (worker -> scheduler).
	taskResultSendSeconds     prometheus.Histogram
	taskResultSendErrorsTotal *prometheus.CounterVec

	handlerPhaseSeconds  *prometheus.HistogramVec
	commSiteWaitSeconds  *prometheus.HistogramVec
	slotPhaseSeconds     *prometheus.CounterVec
	slotReadyWaitSeconds *prometheus.HistogramVec
	jobHandoffSeconds    *prometheus.HistogramVec

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

	// lock holds the instruments for the worker's observed mutex.
	lock *obslock.Metrics
}

func newMetrics() *metrics {
	reg := prometheus.NewRegistry()

	return &metrics{
		reg: reg,
		lock: obslock.NewMetrics(reg,
			"loki_engine_worker_lock_wait_seconds",
			"loki_engine_worker_lock_hold_seconds",
		),

		tasksAssignedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_worker_tasks_assigned_total",
			Help: "Total number of tasks assigned to the worker",
		}),
		rejectedAssignmentsTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_worker_rejected_assignments_total",
			Help: "Total number of task assignments the worker rejected because no thread slot was available (worker-side counterpart to the scheduler's assignment_backoffs_total)",
		}),
		taskExecSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_worker_task_exec_seconds",
			Help: "Number of seconds a task took to complete successfully",
		}, []string{"task_type"}),

		passReadSeconds: newNativeHistogram(reg, prometheus.HistogramOpts{
			Name: "loki_engine_worker_pass_read_seconds",
			Help: "Duration of a single read-phase pass",
		}),
		passSendSeconds: newNativeHistogram(reg, prometheus.HistogramOpts{
			Name: "loki_engine_worker_pass_send_seconds",
			Help: "Duration of a single send-phase pass",
		}),

		taskOpenSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_worker_task_open_seconds",
			Help: "Total time spent opening a task's pipeline (Pipeline.Open)",
		}, []string{"task_type"}),
		taskReadSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_worker_task_read_seconds",
			Help: "Total time spent in the read phase (Pipeline.Read) for a task",
		}, []string{"task_type"}),
		taskSendSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_worker_task_send_seconds",
			Help: "Total time spent in the send phase for a task",
		}, []string{"task_type"}),

		setupSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_worker_setup_seconds",
			Help: "Time spent preparing a task for execution before its pipeline is drained",
		}, []string{"task_type"}),

		taskResultSendSeconds: newNativeHistogram(reg, prometheus.HistogramOpts{
			Name: "loki_engine_worker_task_result_send_seconds",
			Help: "Time spent sending a terminal task result to the scheduler and waiting for acknowledgement",
		}),
		taskResultSendErrorsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_worker_task_result_send_errors_total",
			Help: "Total number of failures sending a terminal task result to the scheduler, by error class",
		}, []string{"error_class"}),
		handlerPhaseSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_worker_handler_phase_seconds",
			Help: "Time spent in bounded worker handler phases",
		}, []string{"message_type", "phase", "outcome"}),
		commSiteWaitSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_worker_comm_site_wait_seconds",
			Help: "Time a worker task spent waiting at a bounded communication site",
		}, []string{"site", "mode", "message_type", "task_type", "outcome"}),
		slotPhaseSeconds: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_worker_slot_phase_seconds_total",
			Help: "Worker slot busy wall time partitioned into time the main task goroutine spent blocked on communication versus computing",
		}, []string{"phase", "task_type", "outcome"}),
		slotReadyWaitSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_worker_slot_ready_wait_seconds",
			Help: "Time from a worker slot becoming ready until it receives a job or exits",
		}, []string{"outcome"}),
		jobHandoffSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_worker_job_handoff_seconds",
			Help: "Time for the TaskAssign handler to hand a job to a ready worker thread",
		}, []string{"outcome"}),

		pagesDownloadedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_worker_pages_downloaded_total",
			Help: "Total number of pages downloaded from object storage during task execution",
		}),
		pagesPrunedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_worker_pages_pruned_total",
			Help: "Total number of pages pruned via metadata before download",
		}),
		bytesDownloadedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_worker_bytes_downloaded_total",
			Help: "Total number of bytes downloaded from object storage during task execution",
		}),

		operatorSelfSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_worker_operator_self_seconds",
			Help: "Per-operator wall-clock (not CPU) self-time, exclusive of child operators, by operator type",
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

func newNativeHistogramVec(r prometheus.Registerer, opts prometheus.HistogramOpts, labels []string) *prometheus.HistogramVec {
	applyNativeHistogramOpts(&opts)
	return promauto.With(r).NewHistogramVec(opts, labels)
}

func newNativeHistogram(r prometheus.Registerer, opts prometheus.HistogramOpts) prometheus.Histogram {
	applyNativeHistogramOpts(&opts)
	return promauto.With(r).NewHistogram(opts)
}

func applyNativeHistogramOpts(opts *prometheus.HistogramOpts) {
	opts.NativeHistogramBucketFactor = 1.1
	opts.NativeHistogramMaxBucketNumber = 100
	opts.NativeHistogramMinResetDuration = time.Hour
}

func (m *metrics) startHandler(kind wire.MessageKind) *metrictimer.Timer {
	if m == nil {
		return nil
	}
	messageType := kind.String()
	return metrictimer.New(metrictimer.Config{
		Total:    phaseTotal,
		HasTotal: true,
		Rows:     4,
		Emit: func(phase metrictimer.Phase, outcome metrictimer.Outcome, d time.Duration) {
			m.handlerPhaseSeconds.WithLabelValues(messageType, phase.String(), outcome.String()).Observe(d.Seconds())
		},
	})
}

func handlerOutcome(err error) metrictimer.Outcome {
	if err != nil {
		return outcomeNack
	}
	return outcomeAck
}

// timeCommSite times fn as a single blocking call at a bounded communication
// site, recording its wait and outcome to comm_site_wait_seconds. It returns
// the measured duration so callers can also fold it into slot utilization, plus
// fn's error. A nil *metrics still runs fn. site is used at exactly one call
// site apiece, so it is passed as a plain string rather than a named enum.
// comm_site_wait_seconds is the per-site attribution layer; slot
// comm-vs-compute utilization is accumulated separately on the main runJob
// goroutine.
func (m *metrics) timeCommSite(site string, mode sendMode, messageType wire.MessageKind, taskType taskType, fn func() error) (time.Duration, error) {
	d, err := metrictimer.Time(fn)
	if m != nil {
		m.observeCommSite(site, mode, messageType, taskType, commOutcome(err), d)
	}
	return d, err
}

// timeSend times a fire-and-forget or acknowledged send at a communication
// site: it sends msg to sender (asynchronously when mode is async, otherwise
// synchronously) and records the wait to comm_site_wait_seconds, deriving the
// message_type label from msg. It is the send-shaped counterpart to
// timeCommSite, sparing pure-send sites a one-line callback.
func (m *metrics) timeSend(ctx context.Context, sender *wire.Peer, site string, mode sendMode, taskType taskType, msg wire.Message) (time.Duration, error) {
	return m.timeCommSite(site, mode, msg.Kind(), taskType, func() error {
		if mode == sendModeAsync {
			return sender.SendMessageAsync(ctx, msg)
		}
		return sender.SendMessage(ctx, msg)
	})
}

func (m *metrics) observeCommSite(site string, mode sendMode, messageType wire.MessageKind, taskType taskType, outcome metrictimer.Outcome, d time.Duration) {
	m.commSiteWaitSeconds.WithLabelValues(site, mode.String(), messageType.String(), taskType.String(), outcome.String()).Observe(d.Seconds())
}

func (m *metrics) addSlotPhase(phase metrictimer.Phase, taskType taskType, outcome metrictimer.Outcome, d time.Duration) {
	if d <= 0 {
		return
	}
	m.slotPhaseSeconds.WithLabelValues(phase.String(), taskType.String(), outcome.String()).Add(d.Seconds())
}

func (m *metrics) observeSlotReadyWait(outcome metrictimer.Outcome, d time.Duration) {
	m.slotReadyWaitSeconds.WithLabelValues(outcome.String()).Observe(d.Seconds())
}

func (m *metrics) observeJobHandoff(outcome metrictimer.Outcome, d time.Duration) {
	m.jobHandoffSeconds.WithLabelValues(outcome.String()).Observe(d.Seconds())
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
