package jobqueue

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type queueMetrics struct {
	jobsSent      prometheus.Counter
	jobsDeQueued  prometheus.Counter
	jobsProcessed prometheus.Counter
	jobRetries    *prometheus.CounterVec
	jobsDropped   prometheus.Counter
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

	return &m
}

type workerMetrics struct {
	jobsProcessed *prometheus.CounterVec
}

func newWorkerMetrics(r prometheus.Registerer) *workerMetrics {
	m := workerMetrics{}

	m.jobsProcessed = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "compactor_worker_job_processed_attempts",
		Help:      "Number of jobs processed by worker with their processing status",
	}, []string{"status"})

	return &m
}
