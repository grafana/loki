package compactor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the logline compactor.
type Metrics struct {
	jobsRequested  prometheus.Counter
	jobsReceived   prometheus.Counter
	jobsCompleted  prometheus.Counter
	jobsFailed     prometheus.Counter
	jobsExpired    prometheus.Counter
	compactionTime prometheus.Histogram

	// Per-phase durations for diagnosing compaction bottlenecks.
	mergeDuration  prometheus.Histogram
	uploadDuration prometheus.Histogram

	// Data volume counters for sizing and throughput tracking.
	bytesUploaded prometheus.Counter
}

// NewMetrics creates a new Metrics instance registered with reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		jobsRequested: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_compactor_jobs_requested_total",
			Help: "Total number of job requests sent to the scheduler",
		}),
		jobsReceived: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_compactor_jobs_received_total",
			Help: "Total number of compaction jobs received from the scheduler",
		}),
		jobsCompleted: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_compactor_jobs_completed_total",
			Help: "Total number of compaction jobs completed successfully",
		}),
		jobsFailed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_compactor_jobs_failed_total",
			Help: "Total number of compaction jobs that failed",
		}),
		jobsExpired: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_compactor_jobs_expired_total",
			Help: "Total number of compaction jobs abandoned due to expiration",
		}),
		compactionTime: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_index_compactor_compaction_duration_seconds",
			Help:    "Wall-clock time of each complete compaction job in seconds",
			Buckets: []float64{1, 2, 5, 10, 15, 20, 30, 45, 60, 80, 100, 120, 180, 300, 600, 900, 1200},
		}),
		mergeDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_index_compactor_merge_duration_seconds",
			Help:    "Time spent merging source indexes into the output index",
			Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 120, 180, 300, 600},
		}),
		uploadDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_index_compactor_upload_duration_seconds",
			Help:    "Time spent uploading the merged index to object storage",
			Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 120, 180},
		}),
		bytesUploaded: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_compactor_bytes_uploaded_total",
			Help: "Total bytes written to object storage after compaction",
		}),
	}
}
