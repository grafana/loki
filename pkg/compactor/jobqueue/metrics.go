package jobqueue

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type queueMetrics struct {
	jobsSent               prometheus.Counter
	jobsDeQueued           prometheus.Counter
	jobsProcessed          prometheus.Counter
	jobRetries             *prometheus.CounterVec
	jobsDropped            prometheus.Counter
	jobsProcessingDuration prometheus.Histogram
}

func newQueueMetrics(r prometheus.Registerer) *queueMetrics {
	m := queueMetrics{}

	m.jobsSent = promauto.With(r).NewCounter(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "compactor_jobs_queued_total",
		Help:      "Number of jobs sent to the worker for processing",
	})
	m.jobsProcessed = promauto.With(r).NewCounter(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "compactor_jobs_processed_total",
		Help:      "Total number of jobs successfully processed",
	})
	m.jobRetries = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "compactor_job_retries_total",
		Help:      "Total number of job retries due to various reasons",
	}, []string{"reason"})
	m.jobsDropped = promauto.With(r).NewCounter(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "compactor_jobs_dropped_total",
		Help:      "Total number of jobs dropped after running out of retry attempts",
	})
	m.jobsProcessingDuration = promauto.With(r).NewHistogram(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "compactor_jobs_processing_duration_seconds",
		Help:      "Duration of job processing in seconds",
	})

	return &m
}

type workerMetrics struct {
	jobsProcessed              *prometheus.CounterVec
	workerConnectedToCompactor prometheus.GaugeFunc
}

func newWorkerMetrics(r prometheus.Registerer, allWorkersConnectedToCompactor func() bool) *workerMetrics {
	m := workerMetrics{}

	m.jobsProcessed = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "compactor_worker_job_processed_attempts",
		Help:      "Number of jobs processed by worker with their processing status",
	}, []string{"status"})
	m.workerConnectedToCompactor = promauto.With(r).NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: constants.Loki,
		Name:      "compactor_worker_connected_to_compactor",
		Help:      "Tracks whether all compactor workers are connected to the compactor",
	}, func() float64 {
		if allWorkersConnectedToCompactor() {
			return 1
		}
		return 0
	})

	return &m
}
