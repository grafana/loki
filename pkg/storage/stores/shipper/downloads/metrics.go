package downloads

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/instrument"
)

const (
	statusFailure = "failure"
	statusSuccess = "success"
)

type metrics struct {
	queryTimeTableDownloadDurationSeconds  *prometheus.CounterVec
	tablesSyncOperationTotal               *prometheus.CounterVec
	tablesDownloadOperationDurationSeconds prometheus.Gauge
	ensureQueryReadinessDurationSeconds    prometheus.Histogram
	usersToBeQueryReadyForTotal            prometheus.Gauge
}

func newMetrics(r prometheus.Registerer) *metrics {
	m := &metrics{
		queryTimeTableDownloadDurationSeconds: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "query_time_table_download_duration_seconds",
			Help:      "Time (in seconds) spent in downloading of files per table at query time",
		}, []string{"table"}),
		ensureQueryReadinessDurationSeconds: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "query_readiness_duration_seconds",
			Help:      "Time (in seconds) spent making an index gateway ready to be queried",
			Buckets:   instrument.DefBuckets,
		}),
		usersToBeQueryReadyForTotal: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "users_to_be_query_ready_for_total",
			Help:      "Total number of users an index gateway instance has to be query ready for.",
		}),
		tablesSyncOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "tables_sync_operation_total",
			Help:      "Total number of tables sync operations done by status",
		}, []string{"status"}),
		tablesDownloadOperationDurationSeconds: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "tables_download_operation_duration_seconds",
			Help:      "Time (in seconds) spent in downloading updated files for all the tables",
		}),
	}

	return m
}
