package compactor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	statusFailure = "failure"
	statusSuccess = "success"

	lblWithRetention = "with_retention"
)

type metrics struct {
	compactTablesOperationTotal           *prometheus.CounterVec
	compactTablesOperationDurationSeconds *prometheus.GaugeVec
	compactTablesOperationLastSuccess     *prometheus.GaugeVec
	applyRetentionLastSuccess             prometheus.Gauge
	compactorRunning                      prometheus.Gauge
	skippedCompactingLockedTables         prometheus.Counter
}

func newMetrics(r prometheus.Registerer) *metrics {
	m := metrics{
		compactTablesOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "compact_tables_operation_total",
			Help:      "Total number of tables compaction done by status and with/without retention",
		}, []string{"status", lblWithRetention}),
		compactTablesOperationDurationSeconds: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "compact_tables_operation_duration_seconds",
			Help:      "Time (in seconds) spent in compacting all the tables with/without retention",
		}, []string{lblWithRetention}),
		compactTablesOperationLastSuccess: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "compact_tables_operation_last_successful_run_timestamp_seconds",
			Help:      "Unix timestamp of the last successful compaction run",
		}, []string{lblWithRetention}),
		applyRetentionLastSuccess: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "apply_retention_last_successful_run_timestamp_seconds",
			Help:      "Unix timestamp of the last successful retention run",
		}),
		compactorRunning: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "compactor_running",
			Help:      "Value will be 1 if compactor is currently running on this instance",
		}),
		skippedCompactingLockedTables: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: "loki_compactor",
			Name:      "skipped_compacting_locked_tables_total",
			Help:      "Count of uncompacted tables being skipped due to them being locked by retention",
		}),
	}

	return &m
}
