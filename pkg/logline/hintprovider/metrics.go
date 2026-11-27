package hintprovider

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type cacheMetrics struct {
	entries   prometheus.Gauge
	sizeBytes prometheus.Gauge
	drops     prometheus.Counter
	evictions prometheus.Counter
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
			Help: "Total number of metadata cache inserts dropped because the entry alone is larger than the cache's byte budget.",
		}),
		evictions: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_hint_provider_metadata_cache_evictions_total",
			Help: "Total number of least recently used metadata cache entries evicted to stay within the entry cap or byte budget.",
		}),
	}
}
