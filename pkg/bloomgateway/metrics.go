package bloomgateway

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type metrics struct {
	*workerMetrics
	*serverMetrics
}

type serverMetrics struct {
	queueDuration    prometheus.Histogram
	inflightRequests prometheus.Summary
	chunkRemovals    *prometheus.CounterVec
}

func newMetrics(registerer prometheus.Registerer, namespace, subsystem string) *metrics {
	return &metrics{
		workerMetrics: newWorkerMetrics(registerer, namespace, subsystem),
		serverMetrics: newServerMetrics(registerer, namespace, subsystem),
	}
}

func newServerMetrics(registerer prometheus.Registerer, namespace, subsystem string) *serverMetrics {
	return &serverMetrics{
		queueDuration: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "queue_duration_seconds",
			Help:      "Time spent by tasks in queue before getting picked up by a worker.",
			Buckets:   prometheus.DefBuckets,
		}),
		inflightRequests: promauto.With(registerer).NewSummary(prometheus.SummaryOpts{
			Namespace:  namespace,
			Subsystem:  subsystem,
			Name:       "inflight_tasks",
			Help:       "Number of inflight tasks (either queued or processing) sampled at a regular interval. Quantile buckets keep track of inflight tasks over the last 60s.",
			Objectives: map[float64]float64{0.5: 0.05, 0.75: 0.02, 0.8: 0.02, 0.9: 0.01, 0.95: 0.01, 0.99: 0.001},
			MaxAge:     time.Minute,
			AgeBuckets: 6,
		}),
		chunkRemovals: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "chunk_removals_total",
			Help:      "Total amount of removals received from the block querier partitioned by state. The state 'accepted' means that the removals are processed, the state 'dropped' means that the removals were received after the task context was done (e.g. client timeout, etc).",
		}, []string{"state"}),
	}
}

type workerMetrics struct {
	dequeueDuration   *prometheus.HistogramVec
	processDuration   *prometheus.HistogramVec
	metasFetched      *prometheus.HistogramVec
	blocksFetched     *prometheus.HistogramVec
	tasksDequeued     *prometheus.CounterVec
	tasksProcessed    *prometheus.CounterVec
	blockQueryLatency *prometheus.HistogramVec
}

func newWorkerMetrics(registerer prometheus.Registerer, namespace, subsystem string) *workerMetrics {
	labels := []string{"worker"}
	r := promauto.With(registerer)
	return &workerMetrics{
		dequeueDuration: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "dequeue_duration_seconds",
			Help:      "Time spent dequeuing tasks from queue in seconds",
		}, labels),
		processDuration: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "process_duration_seconds",
			Help:      "Time spent processing tasks in seconds",
		}, append(labels, "status")),
		metasFetched: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "metas_fetched",
			Help:      "Amount of metas fetched",
		}, labels),
		blocksFetched: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "blocks_fetched",
			Help:      "Amount of blocks fetched",
		}, labels),
		tasksDequeued: r.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "tasks_dequeued_total",
			Help:      "Total amount of tasks that the worker dequeued from the queue",
		}, append(labels, "status")),
		tasksProcessed: r.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "tasks_processed_total",
			Help:      "Total amount of tasks that the worker processed",
		}, append(labels, "status")),
		blockQueryLatency: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "block_query_latency",
			Help:      "Time spent running searches against a bloom block",
		}, append(labels, "status")),
	}
}
