package metadata

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Values of the operation label of the errors counter.
const (
	operationFetch = "fetch"
	operationStore = "store"
)

var (
	kinds      = []string{KindLogsCache, KindIndexCache, KindTOCCache}
	operations = []string{operationFetch, operationStore}
)

// CacheMetrics holds the counters that the Caches on one registry share. Each Cache counts under
// its own kind.
type CacheMetrics struct {
	hits   *prometheus.CounterVec
	misses *prometheus.CounterVec
	errors *prometheus.CounterVec
}

// NewCacheMetrics registers the counters of the Caches on reg. reg may be nil. Create it once for
// each registry, because it registers counters.
func NewCacheMetrics(reg prometheus.Registerer) *CacheMetrics {
	m := &CacheMetrics{
		hits: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_cache_hits_total",
			Help: "Data-object metadata cache entries served from the cache.",
		}, []string{"kind"}),
		misses: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_cache_misses_total",
			Help: "Data-object metadata cache lookups that found no entry, or an entry that the caller rejected, and then loaded one from object storage. A cache fetch error counts in the errors metric, not here.",
		}, []string{"kind"}),
		errors: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_cache_errors_total",
			Help: "Data-object metadata cache errors by operation (fetch, store).",
		}, []string{"kind", "operation"}),
	}

	// Pre-create the series of every kind and operation so a healthy process still exposes a zero
	// series for each; a metric that only appears on the first error is indistinguishable from a
	// scrape gap.
	for _, kind := range kinds {
		m.hits.WithLabelValues(kind)
		m.misses.WithLabelValues(kind)
		for _, operation := range operations {
			m.errors.WithLabelValues(kind, operation)
		}
	}

	return m
}
