package metastore

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Values of the result label of the lookups metric.
const (
	lookupHit   = "hit"
	lookupStale = "stale"
	lookupMiss  = "miss"
)

// Values of the result label of the refresh metrics.
const (
	refreshOK       = "ok"
	refreshError    = "error"
	refreshNotFound = "not_found"
)

// Values of the source and trigger labels of the loads metric.
const (
	sourceCache  = "cache"
	sourceBucket = "bucket"

	triggerMiss    = "miss"
	triggerRefresh = "refresh"
)

// Values of the reason label of the evictions metric.
const (
	// evictionInactive labels the ToCs that no query requested within the active window.
	evictionInactive = "inactive"

	// evictionSize labels the ToCs that a refresh dropped to free space.
	evictionSize = "size"

	// evictionRejected labels the new ToCs that did not fit in the size limit, so they never entered memory.
	evictionRejected = "rejected"

	// evictionOversized labels the ToCs that left memory because their new value did not fit in the size limit.
	evictionOversized = "oversized"
)

type tocResolverMetrics struct {
	lookups         *prometheus.CounterVec
	loads           *prometheus.CounterVec
	refreshes       *prometheus.CounterVec
	refreshDuration *prometheus.HistogramVec
	evictions       *prometheus.CounterVec
}

// newTOCResolverMetrics registers the metrics of the resolver on reg, which may be nil.
func newTOCResolverMetrics(reg prometheus.Registerer) *tocResolverMetrics {
	f := promauto.With(reg)

	m := &tocResolverMetrics{
		lookups: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_metastore_toc_lookups_total",
			Help: "Total number of ToC lookups of queries by result. A hit is served from memory. A stale lookup finds a ToC older than twice the refresh interval, or one stamped more than a few seconds ahead of the clock. A miss finds none. Both read object storage.",
		}, []string{"result"}),
		loads: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_metastore_toc_loads_total",
			Help: "Total number of ToC loads by the source of the bytes and by trigger. The miss trigger is a lookup that found no ToC or a stale one. The refresh trigger is a background refresh, which reads the shared cache first when there is one. A failed read is not counted. Lookups of one ToC that run at the same time share one load.",
		}, []string{"source", "trigger"}),
		refreshes: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_metastore_toc_refreshes_total",
			Help: "Total number of background ToC refreshes by result: ok, not_found when the ToC does not exist, or error.",
		}, []string{"result"}),
		refreshDuration: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_metastore_toc_refresh_duration_seconds",
			Help:                            "Time taken to refresh one ToC in seconds.",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{"result"}),
		evictions: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_metastore_toc_evictions_total",
			Help: "Total number of ToCs that left memory or that the resolver did not store, by reason. Inactive: no query requested the ToC within the active window. Size: a refresh dropped the ToC to free space. Oversized: the new value of the ToC did not fit in the size limit. Rejected: a new ToC did not fit in the size limit. A rejected ToC counts again each time a lookup loads it from object storage.",
		}, []string{"reason"}),
	}

	// Create every label value now, so a rate reads zero before the first event.
	for _, result := range []string{lookupHit, lookupStale, lookupMiss} {
		m.lookups.WithLabelValues(result)
	}
	for _, source := range []string{sourceCache, sourceBucket} {
		for _, trigger := range []string{triggerMiss, triggerRefresh} {
			m.loads.WithLabelValues(source, trigger)
		}
	}
	for _, result := range []string{refreshOK, refreshError, refreshNotFound} {
		m.refreshes.WithLabelValues(result)
		m.refreshDuration.WithLabelValues(result)
	}
	for _, reason := range []string{evictionInactive, evictionSize, evictionRejected, evictionOversized} {
		m.evictions.WithLabelValues(reason)
	}

	return m
}

// refreshed records one finished refresh.
func (m *tocResolverMetrics) refreshed(result string, d time.Duration) {
	m.refreshes.WithLabelValues(result).Inc()
	m.refreshDuration.WithLabelValues(result).Observe(d.Seconds())
}
