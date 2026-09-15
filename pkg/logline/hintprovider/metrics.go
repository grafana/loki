package hintprovider

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type cacheMetrics struct {
	entries   prometheus.Gauge
	sizeBytes prometheus.Gauge
	drops     prometheus.Counter
}

func newCacheMetrics(reg prometheus.Registerer) *cacheMetrics {
	return &cacheMetrics{
		entries: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_hint_provider_metadata_cache_entries",
			Help: "Current number of metadata cache entries.",
		}),
		sizeBytes: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_hint_provider_metadata_cache_bytes",
			Help: "Estimated in-memory bytes held by metadata cache entries.",
		}),
		drops: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_hint_provider_metadata_cache_drops_total",
			Help: "Total number of metadata cache inserts dropped due to capacity limits.",
		}),
	}
}
