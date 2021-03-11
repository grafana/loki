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

type downloadTableBytesMetric struct {
	sync.RWMutex
	gauge   prometheus.Gauge
	periods map[string]int64
}

func (m *downloadTableBytesMetric) add(period string, downloadedBytes int64) {
	m.Lock()
	defer m.Unlock()
	m.periods[period] = downloadedBytes

	totalDownloadedBytes := int64(0)
	for _, downloadedBytes := range m.periods {
		totalDownloadedBytes += downloadedBytes
	}

	m.gauge.Set(float64(totalDownloadedBytes))
}

type metrics struct {
	// metrics for measuring performance of downloading of files per period initially i.e for the first time
	tablesDownloadDurationSeconds *downloadTableDurationMetric
	tablesDownloadSizeBytes       *downloadTableBytesMetric

	tablesSyncOperationTotal *prometheus.CounterVec
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
		tablesDownloadSizeBytes: &downloadTableBytesMetric{
			periods: map[string]int64{},
			gauge: promauto.With(r).NewGauge(prometheus.GaugeOpts{
				Namespace: "loki_boltdb_shipper",
				Name:      "initial_tables_download_size_bytes",
				Help:      "Size of files (in bytes) downloaded per table, initially i.e for the first time",
			})},
		tablesSyncOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "tables_sync_operation_total",
			Help:      "Total number of tables sync operations done by status",
		}, []string{"status"}),
	}

	return m
}
