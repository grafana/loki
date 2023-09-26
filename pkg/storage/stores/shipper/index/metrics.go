package index

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type metrics struct {
	openExistingFileFailuresTotal prometheus.Counter
}

func newMetrics(r prometheus.Registerer) *metrics {
	return &metrics{
		openExistingFileFailuresTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "open_existing_file_failures_total",
			Help:      "Total number of failures in opening of existing files while loading active index tables during startup",
		}),
	}
}
