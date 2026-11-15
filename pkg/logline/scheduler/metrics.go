package scheduler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the logline index scheduler.
type Metrics struct {
	jobsCreatedTotal           prometheus.Counter
	jobsCompletedTotal         *prometheus.CounterVec
	jobsInFlight               prometheus.Gauge
	availableIndexes           prometheus.Gauge
	pendingJobs                prometheus.Gauge
	longPollWaiting            prometheus.Gauge
	jobRequestsTotal           prometheus.Counter
	tickDuration               prometheus.Histogram
	jobsSkippedOversized       prometheus.Gauge
	indexesUnreadable          prometheus.Gauge
	indexesDeletedTotal        prometheus.Counter
	indexesDeleteFailuresTotal prometheus.Counter
}

// NewMetrics creates a new Metrics instance.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		jobsCreatedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_scheduler_jobs_created_total",
			Help: "Total number of compaction jobs created and assigned to workers",
		}),
		jobsCompletedTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "logline_index_scheduler_jobs_completed_total",
			Help: "Total number of compaction jobs completed, by reason (worker_report, state_change, expiration, abandon)",
		}, []string{"reason"}),
		jobsInFlight: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_index_scheduler_jobs_in_flight",
			Help: "Current number of compaction jobs assigned to workers",
		}),
		availableIndexes: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_index_scheduler_available_indexes",
			Help: "Number of active indexes available for compaction (excluding in-flight sources)",
		}),
		pendingJobs: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_index_scheduler_pending_jobs",
			Help: "Number of compaction jobs planned but not yet assigned to a worker.",
		}),
		longPollWaiting: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_index_scheduler_long_poll_waiting",
			Help: "Number of workers currently blocked in long poll waiting for work",
		}),
		jobRequestsTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_scheduler_job_requests_total",
			Help: "Total number of job requests received from workers",
		}),
		tickDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_index_scheduler_tick_duration_seconds",
			Help:    "Duration of each scheduler tick (expire + completion check + planner rebuild) in seconds",
			Buckets: prometheus.DefBuckets,
		}),
		jobsSkippedOversized: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_index_scheduler_jobs_skipped_oversized",
			Help: "Number of indexes in the current plan that could not be compacted because no valid group fits max_job_size_bytes",
		}),
		indexesUnreadable: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_index_scheduler_indexes_unreadable",
			Help: "Number of indexes excluded from compaction because this binary cannot read their format: an unrecognised version, a footer version it refuses, or a v5 meta.json missing the index_header or document_interval needed to group it. Unlike skipped_oversized these never become compactable, so a non-zero value needs operator attention",
		}),
		indexesDeletedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_scheduler_indexes_deleted_total",
			Help: "Total number of indexes deleted from object storage (retention or compacted source cleanup)",
		}),
		indexesDeleteFailuresTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_scheduler_indexes_delete_failures_total",
			Help: "Total number of failed attempts to delete an eligible index from object storage",
		}),
	}
}
