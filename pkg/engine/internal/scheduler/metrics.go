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
	requeueTotal  prometheus.Counter

	taskQueueSeconds prometheus.Histogram
	taskExecSeconds  prometheus.Histogram

	taskAssignmentAttemptsTotal *prometheus.CounterVec
	taskAssignmentSendSeconds   *prometheus.HistogramVec
	taskAssignmentRequeueTotal  *prometheus.CounterVec

	taskStatusMessagesTotal  *prometheus.CounterVec
	taskStatusHandlerSeconds *prometheus.HistogramVec
	taskStatusStaleTotal     *prometheus.CounterVec

	workerSubscriptionsTotal *prometheus.CounterVec
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
		requeueTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_task_requeue_total",
			Help: "Total number of times a task was requeued after a failed assignment attempt",
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

		taskAssignmentAttemptsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_task_assignment_attempts_total",
			Help: "Total number of scheduler task assignment attempts by outcome.",
		}, []string{"outcome"}),
		taskAssignmentSendSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_task_assignment_send_seconds",
			Help: "Time spent sending TaskAssign messages to workers by assignment outcome.",
		}, []string{"outcome"}),
		taskAssignmentRequeueTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_task_assignment_requeue_total",
			Help: "Total number of task assignment requeues by reason.",
		}, []string{"reason"}),

		taskStatusMessagesTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_task_status_messages_total",
			Help: "Total number of scheduler TaskStatus messages by state class and outcome.",
		}, []string{"state_class", "outcome"}),
		taskStatusHandlerSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_task_status_handler_seconds",
			Help: "Time spent handling TaskStatus messages by phase, state class, and outcome.",
		}, []string{"phase", "state_class", "outcome"}),
		taskStatusStaleTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_task_status_stale_total",
			Help: "Total number of stale or ignored TaskStatus messages by reason.",
		}, []string{"reason"}),

		workerSubscriptionsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_worker_subscriptions_total",
			Help: "Total number of WorkerSubscribe messages sent by outcome.",
		}, []string{"outcome"}),
	}
}

// Register registers metrics to report to reg.
func (m *metrics) Register(reg prometheus.Registerer) error { return reg.Register(m.reg) }

// Unregister unregisters metrics from the provided Registerer.
func (m *metrics) Unregister(reg prometheus.Registerer) { reg.Unregister(m.reg) }

// newNativeHistogramVec creates a HistogramVec that uses native histogram
// buckets, registered to reg.
func newNativeHistogramVec(reg prometheus.Registerer, opts prometheus.HistogramOpts, labels []string) *prometheus.HistogramVec {
	opts.NativeHistogramBucketFactor = 1.1
	opts.NativeHistogramMaxBucketNumber = 100
	opts.NativeHistogramMinResetDuration = time.Hour
	return promauto.With(reg).NewHistogramVec(opts, labels)
}
