package downloads

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	statusFailure = "failure"
	statusSuccess = "success"
)

type metrics struct {
	queryTimeTableDownloadDurationSeconds  *prometheus.CounterVec
	tablesSyncOperationTotal               *prometheus.CounterVec
	tablesDownloadOperationDurationSeconds prometheus.Gauge
}

func newMetrics(r prometheus.Registerer) *metrics {
	m := &metrics{
		queryTimeTableDownloadDurationSeconds: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "query_time_table_download_duration_seconds",
			Help: "Time (in seconds) spent in downloading of files per table at query time",
		}, []string{"table"}),
		tablesSyncOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "tables_sync_operation_total",
			Help: "Total number of tables sync operations done by status",
		}, []string{"status"}),
		tablesDownloadOperationDurationSeconds: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "tables_download_operation_duration_seconds",
			Help: "Time (in seconds) spent in downloading updated files for all the tables",
		}),
	}

	return m
}
