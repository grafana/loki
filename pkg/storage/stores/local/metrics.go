package local

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/instrument"
)

const (
	statusFailure = "failure"
	statusSuccess = "success"
)

type boltDBShipperMetrics struct {
	// metrics for measuring performance of downloading of files per period initially i.e for the first time
	initialFilesDownloadDurationSeconds *prometheus.GaugeVec
	initialFilesDownloadSizeBytes       *prometheus.GaugeVec

	// duration in seconds spent in serving request on index managed by BoltDB Shipper
	requestDurationSeconds *prometheus.HistogramVec

	filesDownloadOperationTotal *prometheus.CounterVec
	filesUploadOperationTotal   *prometheus.CounterVec
}

func newBoltDBShipperMetrics(r prometheus.Registerer) *boltDBShipperMetrics {
	m := &boltDBShipperMetrics{
		initialFilesDownloadDurationSeconds: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "initial_files_download_duration_seconds",
			Help:      "Time (in seconds) spent in downloading of files per period, initially i.e for the first time",
		}, []string{"period"}),
		initialFilesDownloadSizeBytes: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "initial_files_download_size_bytes",
			Help:      "Size of files (in bytes) downloaded per period, initially i.e for the first time",
		}, []string{"period"}),
		requestDurationSeconds: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "request_duration_seconds",
			Help:      "Time (in seconds) spent serving requests when using boltdb shipper",
			Buckets:   instrument.DefBuckets,
		}, []string{"operation", "status_code"}),
		filesDownloadOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "files_download_operation_total",
			Help:      "Total number of download operations done by status",
		}, []string{"status"}),
		filesUploadOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "files_upload_operation_total",
			Help:      "Total number of upload operations done by status",
		}, []string{"status"}),
	}

	return m
}
