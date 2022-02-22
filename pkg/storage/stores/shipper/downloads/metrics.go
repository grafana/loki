package downloads

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	statusFailure = "failure"
	statusSuccess = "success"
)

type downloadTableDurationMetric struct {
	sync.RWMutex
	gauge   prometheus.Gauge
	periods map[string]float64
}

func (m *downloadTableDurationMetric) add(period string, downloadDuration float64) {
	m.Lock()
	defer m.Unlock()
	m.periods[period] = downloadDuration

	totalDuration := float64(0)
	for _, dur := range m.periods {
		totalDuration += dur
	}

	m.gauge.Set(totalDuration)
}

type metrics struct {
	// metrics for measuring performance of downloading of files per period initially i.e for the first time
	tablesDownloadDurationSeconds *downloadTableDurationMetric

	tablesSyncOperationTotal           *prometheus.CounterVec
	tablesSyncOperationDurationSeconds prometheus.Gauge
}

func newMetrics(r prometheus.Registerer) *metrics {
	m := &metrics{
		tablesDownloadDurationSeconds: &downloadTableDurationMetric{
			periods: map[string]float64{},
			gauge: promauto.With(r).NewGauge(prometheus.GaugeOpts{
				Namespace: "loki_boltdb_shipper",
				Name:      "initial_tables_download_duration_seconds",
				Help:      "Time (in seconds) spent in downloading of files per table, initially i.e for the first time",
			})},
		tablesSyncOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "tables_sync_operation_total",
			Help:      "Total number of tables sync operations done by status",
		}, []string{"status"}),
		tablesSyncOperationDurationSeconds: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "tables_sync_operation_duration_seconds",
			Help:      "Time (in seconds) spent in syncing all the tables",
		}),
	}

	return m
}
