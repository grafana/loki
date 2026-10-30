package compactor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v4/pkg/util/constants"
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
	return &metrics{
		compactTablesOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "tsdb_shipper_compact_tables_operation_total",
			Help:      "Total number of tables compaction done by status",
		}, []string{"status"}),
		compactTablesOperationDurationSeconds: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "tsdb_shipper_compact_tables_operation_duration_seconds",
			Help:      "Time (in seconds) spent in compacting all the tables",
		}),
		compactTablesOperationLastSuccess: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "tsdb_shipper_compact_tables_operation_last_successful_run_timestamp_seconds",
			Help:      "Unix timestamp of the last successful compaction run",
		}),
		applyRetentionOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "compactor_apply_retention_operation_total",
			Help:      "Total number of attempts done to apply retention with status",
		}, []string{"status"}),
		applyRetentionOperationDurationSeconds: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "compactor_apply_retention_operation_duration_seconds",
			Help:      "Time (in seconds) spent in applying retention",
		}),
		applyRetentionLastSuccess: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "compactor_apply_retention_last_successful_run_timestamp_seconds",
			Help:      "Unix timestamp of the last successful retention run",
		}),
		compactorRunning: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "tsdb_shipper_compactor_running",
			Help:      "Value will be 1 if compactor is currently running on this instance",
		}),
		skippedCompactingLockedTables: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "compactor_locked_table_successive_compaction_skips",
			Help:      "Number of times uncompacted tables were consecutively skipped due to them being locked by retention",
		}, []string{"table_name"}),
	}
}
