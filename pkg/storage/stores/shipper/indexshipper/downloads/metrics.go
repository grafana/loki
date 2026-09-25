package downloads

import (
	"time"

	"github.com/grafana/dskit/instrument"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	statusFailure = "failure"
	statusSuccess = "success"
)

type metrics struct {
	fileDownloadDuration                   *prometheus.HistogramVec
	queryTimeTableDownloadDurationSeconds  *prometheus.CounterVec
	tablesSyncOperationTotal               *prometheus.CounterVec
	tablesDownloadOperationDurationSeconds *prometheus.GaugeVec

	// new metrics that will supersed the incorrect old types
	queryWaitTime    *prometheus.HistogramVec
	tableSyncLatency *prometheus.HistogramVec
}

func newMetrics(r prometheus.Registerer) *metrics {
	m := &metrics{
		fileDownloadDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "index_file_download_duration_seconds",
			Help:                            "Object retrieval and transfer to a temporary file, excluding extraction, fsync and open. Successful observation count is the number of files downloaded, including repeat downloads.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"status_code"}),
		queryTimeTableDownloadDurationSeconds: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "query_time_table_download_duration_seconds",
			Help: "Time (in seconds) spent in downloading of files per table at query time",
		}, []string{"table"}),
		tablesSyncOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "tables_sync_operation_total",
			Help: "Total number of tables sync operations done by status and trigger",
		}, []string{"status", "trigger"}),
		tablesDownloadOperationDurationSeconds: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Name: "tables_download_operation_duration_seconds",
			Help: "Time (in seconds) spent in downloading updated files for all the tables",
		}, []string{"trigger"}),

		queryWaitTime: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name: "query_wait_time_seconds",
			Help: "Time (in seconds) spent waiting for index files to be queryable at query time",
		}, []string{"table"}),
		tableSyncLatency: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name: "table_sync_latency_seconds",
			Help: "Time (in seconds) spent in downloading updated files for all the tables",
		}, []string{"table", "status", "trigger"}),
	}

	return m
}

func (m *metrics) observeDownload(elapsed time.Duration, err error) {
	m.fileDownloadDuration.WithLabelValues(instrument.ErrorCode(err)).Observe(elapsed.Seconds())
}
