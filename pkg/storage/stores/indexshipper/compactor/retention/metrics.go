package retention

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	statusFailure  = "failure"
	statusSuccess  = "success"
	statusNotFound = "notfound"

	tableActionModified = "modified"
	tableActionDeleted  = "deleted"
	tableActionNone     = "none"
)

type sweeperMetrics struct {
	deleteChunkDurationSeconds *prometheus.HistogramVec
	markerFileCurrentTime      prometheus.Gauge
	markerFilesCurrent         prometheus.Gauge
	markerFilesDeletedTotal    prometheus.Counter
}

func newSweeperMetrics(r prometheus.Registerer) *sweeperMetrics {
	return &sweeperMetrics{
		deleteChunkDurationSeconds: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_sweeper_chunk_deleted_duration_seconds",
			Help:      "Time (in seconds) spent in deleting chunk",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 8),
		}, []string{"status"}),
		markerFilesCurrent: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_sweeper_marker_files_current",
			Help:      "The current total of marker files valid for deletion.",
		}),
		markerFileCurrentTime: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_sweeper_marker_file_processing_current_time",
			Help:      "The current time of creation of the marker file being processed.",
		}),
		markerFilesDeletedTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_sweeper_marker_files_deleted_total",
			Help:      "The total of marker files deleted after being fully processed.",
		}),
	}
}

type MarkerMetrics struct {
	tableProcessedTotal           *prometheus.CounterVec
	tableMarksCreatedTotal        *prometheus.CounterVec
	tableProcessedDurationSeconds *prometheus.HistogramVec
}

func newMarkerMetrics(r prometheus.Registerer) *MarkerMetrics {
	return &MarkerMetrics{
		tableProcessedTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_marker_table_processed_total",
			Help:      "Total amount of table processed for each user per action. Empty string for user_id is for common index",
		}, []string{"table", "user_id", "action"}),
		tableMarksCreatedTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_marker_count_total",
			Help:      "Total count of markers created per table.",
		}, []string{"table"}),
		tableProcessedDurationSeconds: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_marker_table_processed_duration_seconds",
			Help:      "Time (in seconds) spent in marking table for chunks to delete",
			Buckets:   []float64{1, 2.5, 5, 10, 20, 40, 90, 360, 600, 1800},
		}, []string{"table", "status"}),
	}
}

type cleanupMetrics struct {
	chunksProcessedTotal            *prometheus.CounterVec
	chunksDeletedTotal              *prometheus.CounterVec
	cleanupProcessedDurationSeconds *prometheus.HistogramVec
}

func newCleanupMetrics(r prometheus.Registerer) *cleanupMetrics {
	return &cleanupMetrics{
		chunksProcessedTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_cleanup_chunks_processed_total",
			Help:      "Total amount of chunks processed for each user per cleanup. Empty string for user_id is for common index",
		}, []string{"table", "user_id", "action"}),
		chunksDeletedTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_cleanup_chunks_deleted_count_total",
			Help:      "Total count of chunks deleted per table.",
		}, []string{"table"}),
		cleanupProcessedDurationSeconds: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_cleanup_table_processed_duration_seconds",
			Help:      "Time (in seconds) spent in deleting table chunks",
			Buckets:   []float64{1, 2.5, 5, 10, 20, 40, 90, 360, 600, 1800},
		}, []string{"table", "status"}),
	}
}
