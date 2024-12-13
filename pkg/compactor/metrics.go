package compactor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	statusFailure = "failure"
	statusSuccess = "success"
)

type metrics struct {
	compactTablesOperationTotal            *prometheus.CounterVec
	compactTablesOperationDurationSeconds  prometheus.Gauge
	compactTablesOperationLastSuccess      prometheus.Gauge
	applyRetentionOperationTotal           *prometheus.CounterVec
	applyRetentionOperationDurationSeconds prometheus.Gauge
	applyRetentionLastSuccess              prometheus.Gauge
	compactorRunning                       prometheus.Gauge
	skippedCompactingLockedTables          *prometheus.GaugeVec
}

func newMetrics(r prometheus.Registerer) *metrics {
	m := metrics{
		compactTablesOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "compact_tables_operation_total",
			Help:      "Total number of tables compaction done by status",
		}, []string{"status"}),
		compactTablesOperationDurationSeconds: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "compact_tables_operation_duration_seconds",
			Help:      "Time (in seconds) spent in compacting all the tables",
		}),
		compactTablesOperationLastSuccess: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "compact_tables_operation_last_successful_run_timestamp_seconds",
			Help:      "Unix timestamp of the last successful compaction run",
		}),
		applyRetentionOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_compactor",
			Name:      "apply_retention_operation_total",
			Help:      "Total number of attempts done to apply retention with status",
		}, []string{"status"}),
		applyRetentionOperationDurationSeconds: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_compactor",
			Name:      "apply_retention_operation_duration_seconds",
			Help:      "Time (in seconds) spent in applying retention",
		}),
		applyRetentionLastSuccess: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_compactor",
			Name:      "apply_retention_last_successful_run_timestamp_seconds",
			Help:      "Unix timestamp of the last successful retention run",
		}),
		compactorRunning: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "compactor_running",
			Help:      "Value will be 1 if compactor is currently running on this instance",
		}),
		skippedCompactingLockedTables: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "loki_compactor",
			Name:      "locked_table_successive_compaction_skips",
			Help:      "Number of times uncompacted tables were consecutively skipped due to them being locked by retention",
		}, []string{"table_name"}),
	}

	return &m
}
