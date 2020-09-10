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
	compactTablesOperationTotal *prometheus.CounterVec
	compactTablesOperationDurationSeconds prometheus.Gauge
}

func newMetrics(r prometheus.Registerer) *metrics {
	m := metrics{
		compactTablesOperationTotal:           promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "compact_tables_operation_total",
			Help:      "Total number of tables compaction done by status",
		}, []string{"status"}),
		compactTablesOperationDurationSeconds: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "compact_tables_operation_duration_seconds",
			Help:      "Time (in seconds) spent in compacting all the tables",
		}),
	}

	return &m
}
