package scheduler

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/engine/internal/metrictimer"
	"github.com/grafana/loki/v3/pkg/engine/internal/obslock"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

// metrics is a container of metrics for a scheduler.
type metrics struct {
	// registry to collect metrics as a unit.
	reg *prometheus.Registry

	tasksTotal    *prometheus.CounterVec
	streamsTotal  *prometheus.CounterVec
	connsTotal    prometheus.Counter
	backoffsTotal prometheus.Counter
	requeueTotal  prometheus.Counter

	taskQueueSeconds prometheus.Histogram
	taskExecSeconds  prometheus.Histogram

	handlerPhaseSeconds     *prometheus.HistogramVec
	assignmentHopSeconds    *prometheus.HistogramVec
	assignmentAttemptsTotal *prometheus.CounterVec

	// lock holds the instruments for the scheduler's observed mutexes.
	lock *obslock.Metrics

	// activeLoad tracks the number of tasks currently in a load-bearing state
	// (pending or running). It is maintained incrementally by
	// [task.setStateLocked] on every state transition so that the load gauge can
	// be read without scanning every task. See [computeLoad].
	activeLoad atomic.Int64
}

// Handler phase label values shared by multiple handlers. Phase values used at a
// single call site are inlined as string literals there.
const (
	phaseTotal metrictimer.Phase = "total"
)

// Outcome label values.
const (
	outcomeAck          metrictimer.Outcome = "ack"
	outcomeNack         metrictimer.Outcome = "nack"
	outcomeSuccess      metrictimer.Outcome = "success"
	outcomeEmpty        metrictimer.Outcome = "empty"
	outcomeCanceled     metrictimer.Outcome = "canceled"
	outcomeConnClosed   metrictimer.Outcome = "conn_closed"
	outcomeAssigned     metrictimer.Outcome = "assigned"
	outcomeReady        metrictimer.Outcome = "ready"
	outcomeUnassignable metrictimer.Outcome = "unassignable"
	outcomeNack429      metrictimer.Outcome = "nack_429"
	outcomeTimeout      metrictimer.Outcome = "timeout"
	outcomeSendError    metrictimer.Outcome = "send_error"
)

func newMetrics() *metrics {
	reg := prometheus.NewRegistry()

	return &metrics{
		reg: reg,
		lock: obslock.NewMetrics(reg,
			"loki_engine_scheduler_lock_wait_seconds",
			"loki_engine_scheduler_lock_hold_seconds",
		),

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
		requeueTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_task_requeue_total",
			Help: "Total number of times a task was requeued after a failed assignment attempt",
		}),

		taskQueueSeconds: newNativeHistogram(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_task_queue_seconds",
			Help: "Number of seconds a task sat in a queue before being assigned to a worker thread",
		}),

		taskExecSeconds: newNativeHistogram(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_task_exec_seconds",
			Help: "Number of seconds a task took to complete successfully",
		}),

		handlerPhaseSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_handler_phase_seconds",
			Help: "Time spent in bounded scheduler handler phases",
		}, []string{"message_type", "phase", "outcome"}),
		assignmentHopSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_assignment_hop_seconds",
			Help: "Time spent in scheduler task-assignment hops",
		}, []string{"phase", "outcome"}),
		assignmentAttemptsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_assignment_attempts_total",
			Help: "Total number of scheduler TaskAssign attempts by outcome",
		}, []string{"outcome"}),
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

func (m *metrics) startAssignmentHop(phase metrictimer.Phase) *metrictimer.Timer {
	if m == nil {
		return nil
	}
	return metrictimer.New(metrictimer.Config{
		Total:    phase,
		HasTotal: true,
		Rows:     1,
		Emit: func(p metrictimer.Phase, outcome metrictimer.Outcome, d time.Duration) {
			m.assignmentHopSeconds.WithLabelValues(p.String(), outcome.String()).Observe(d.Seconds())
		},
	})
}

func (m *metrics) incAssignmentAttempt(outcome metrictimer.Outcome) {
	m.assignmentAttemptsTotal.WithLabelValues(outcome.String()).Inc()
}

// Register registers metrics to report to reg.
func (m *metrics) Register(reg prometheus.Registerer) error { return reg.Register(m.reg) }

// Unregister unregisters metrics from the provided Registerer.
func (m *metrics) Unregister(reg prometheus.Registerer) { reg.Unregister(m.reg) }
