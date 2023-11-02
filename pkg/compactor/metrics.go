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
	compactTablesOperationTotal           *prometheus.CounterVec
	compactTablesOperationDurationSeconds prometheus.Gauge
	compactTablesOperationLastSuccess     prometheus.Gauge
	applyRetentionLastSuccess             prometheus.Gauge
	compactorRunning                      prometheus.Gauge
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
	}

	return &m
}
