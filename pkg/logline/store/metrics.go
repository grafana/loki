package store

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds Prometheus metrics for the index store.
type Metrics struct {
	pollDuration prometheus.Histogram
	pollErrors   prometheus.Counter
	indexes      *prometheus.GaugeVec
	indexBytes   *prometheus.GaugeVec
}

// NewMetrics creates a new Metrics instance registered with reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		pollDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_index_store_poll_duration_seconds",
			Help:    "Duration of store poll cycles in seconds.",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 180},
		}),
		pollErrors: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_store_poll_errors_total",
			Help: "Total number of failed poll cycles.",
		}),
		indexes: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "logline_index_store_indexes",
			Help: "Number of indexes in storage by state and date.",
		}, []string{"state", "date"}),
		indexBytes: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "logline_index_store_index_bytes",
			Help: "Bytes stored in object storage per date and state.",
		}, []string{"state", "date"}),
	}
}
