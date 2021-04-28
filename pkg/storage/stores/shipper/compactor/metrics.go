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

	retentionOperationTotal           *prometheus.CounterVec
	retentionOperationDurationSeconds prometheus.Gauge
	retentionOperationLastSuccess     prometheus.Gauge
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

		retentionOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_operation_total",
			Help:      "Total number of retention applied by status",
		}, []string{"status"}),
		retentionOperationDurationSeconds: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_operation_duration_seconds",
			Help:      "Time (in seconds) spent in applying retention for all the tables",
		}),
		retentionOperationLastSuccess: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "retention_operation_last_successful_run_timestamp_seconds",
			Help:      "Unix timestamp of the last successful retention run",
		}),
	}

	return &m
}
