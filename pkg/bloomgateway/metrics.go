package bloomgateway

import (
	"time"

	"github.com/grafana/dskit/instrument"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type metrics struct {
	*workerMetrics
	*serverMetrics
}

const (
	typeSuccess = "success"
	typeError   = "error"
)

type clientMetrics struct {
	clientRequests *prometheus.CounterVec
	requestLatency *prometheus.HistogramVec
	clients        prometheus.Gauge
}

func newClientMetrics(registerer prometheus.Registerer) *clientMetrics {
	return &clientMetrics{
		clientRequests: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: "bloom_gateway_client",
			Name:      "requests_total",
			Help:      "Total number of requests made to the bloom gateway",
		}, []string{"type"}),
		requestLatency: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "bloom_gateway_client",
			Name:      "request_duration_seconds",
			Help:      "Time (in seconds) spent serving requests when using the bloom gateway",
			Buckets:   instrument.DefBuckets,
		}, []string{"operation", "status_code"}),
		clients: promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Subsystem: "bloom_gateway",
			Name:      "clients",
			Help:      "The current number of bloom gateway clients.",
		}),
	}
}

type serverMetrics struct {
	inflightRequests prometheus.Summary
	requestedSeries  prometheus.Histogram
	filteredSeries   prometheus.Histogram
	requestedChunks  prometheus.Histogram
	filteredChunks   prometheus.Histogram
	receivedFilters  prometheus.Histogram
}

func newMetrics(registerer prometheus.Registerer, namespace, subsystem string) *metrics {
	return &metrics{
		workerMetrics: newWorkerMetrics(registerer, namespace, subsystem),
		serverMetrics: newServerMetrics(registerer, namespace, subsystem),
	}
}

func newServerMetrics(registerer prometheus.Registerer, namespace, subsystem string) *serverMetrics {
	return &serverMetrics{
		inflightRequests: promauto.With(registerer).NewSummary(prometheus.SummaryOpts{
			Namespace:  namespace,
			Subsystem:  subsystem,
			Name:       "inflight_tasks",
			Help:       "Number of inflight tasks (either queued or processing) sampled at a regular interval. Quantile buckets keep track of inflight tasks over the last 60s.",
			Objectives: map[float64]float64{0.5: 0.05, 0.75: 0.02, 0.8: 0.02, 0.9: 0.01, 0.95: 0.01, 0.99: 0.001},
			MaxAge:     time.Minute,
			AgeBuckets: 6,
		}),
		requestedSeries: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "requested_series",
			Help:      "Total amount of series refs sent to bloom-gateway for querying",
			Buckets:   prometheus.ExponentialBucketsRange(1, 100e3, 10),
		}),
		filteredSeries: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "filtered_series",
			Help:      "Total amount of series refs filtered by bloom-gateway",
			Buckets:   prometheus.ExponentialBucketsRange(1, 100e3, 10),
		}),
		requestedChunks: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "requested_chunks",
			Help:      "Total amount of chunk refs sent to bloom-gateway for querying",
			Buckets:   prometheus.ExponentialBucketsRange(1, 100e3, 10),
		}),
		filteredChunks: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "filtered_chunks",
			Help:      "Total amount of chunk refs filtered by bloom-gateway",
			Buckets:   prometheus.ExponentialBucketsRange(1, 100e3, 10),
		}),
		receivedFilters: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "request_filters",
			Help:      "Number of filters per request.",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 9), // 1 -> 256
		}),
	}
}

type workerMetrics struct {
	dequeueDuration    *prometheus.HistogramVec
	queueDuration      *prometheus.HistogramVec
	processDuration    *prometheus.HistogramVec
	tasksDequeued      *prometheus.CounterVec
	tasksProcessed     *prometheus.CounterVec
	blocksNotAvailable *prometheus.CounterVec
	blockQueryLatency  *prometheus.HistogramVec
}

func newWorkerMetrics(registerer prometheus.Registerer, namespace, subsystem string) *workerMetrics {
	labels := []string{"worker"}
	r := promauto.With(registerer)
	return &workerMetrics{
		queueDuration: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "queue_duration_seconds",
			Help:      "Time spent by tasks in queue before getting picked up by a worker.",
		}, labels),
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
		blocksNotAvailable: r.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "blocks_not_available_total",
			Help:      "Total amount of blocks that have been skipped because they were not found or not downloaded yet",
		}, labels),
		blockQueryLatency: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "block_query_latency_seconds",
			Help:      "Time spent running searches against a bloom block",
		}, append(labels, "status")),
	}
}
