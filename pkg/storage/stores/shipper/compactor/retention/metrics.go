package retention

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	statusFailure  = "failure"
	statusSuccess  = "success"
	statusNotFound = "notfound"
)

type sweeperMetrics struct {
	deleteChunkTotal           *prometheus.CounterVec
	deleteChunkDurationSeconds *prometheus.HistogramVec
	markerFileCurrentTime      prometheus.Gauge
	markerFilesCurrent         prometheus.Gauge
	markerFilesDeletedTotal    prometheus.Counter
}

func newSweeperMetrics(r prometheus.Registerer) *sweeperMetrics {
	return &sweeperMetrics{
		deleteChunkTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_sweeper_chunk_deleted_total",
			Help:      "Total number of chunks deleted by retention",
		}, []string{"status"}),
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
