package bucketcache

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Values of the origin label of the fetched bytes counter.
const (
	originCache  = "cache"
	originBucket = "bucket"
)

// Values of the operation label of the errors counter.
const (
	operationFetch = "fetch"
	operationStore = "store"
)

type metrics struct {
	requestedBytes     prometheus.Counter
	fetchedBytes       *prometheus.CounterVec
	requestedSubranges prometheus.Counter
	hitSubranges       prometheus.Counter
	corruptedEntries   prometheus.Counter
	errors             *prometheus.CounterVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	m := &metrics{
		requestedBytes: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_bucket_cache_requested_bytes_total",
			Help: "Bytes that readers requested from cached objects.",
		}),
		fetchedBytes: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_bucket_cache_fetched_bytes_total",
			Help: "Bytes of aligned blocks that the cache fetched for reads of cached objects, by origin: the cache, or object storage (and then stored in the cache).",
		}, []string{"origin"}),
		requestedSubranges: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_bucket_cache_requested_subranges_total",
			Help: "Aligned blocks that reads of cached objects asked the cache for.",
		}),
		hitSubranges: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_bucket_cache_hit_subranges_total",
			Help: "Aligned blocks that the cache served.",
		}),
		corruptedEntries: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_bucket_cache_corrupted_entries_total",
			Help: "Cached blocks that the cache rejected because their length or full key was wrong. Each counts as a miss.",
		}),
		errors: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_bucket_cache_errors_total",
			Help: "Bucket cache errors by operation (fetch, store). A failed fetch counts as a miss for the blocks it did not return. A failed store leaves some or all of the blocks uncached. Neither fails a read.",
		}, []string{"operation"}),
	}

	// Create every series at 0, so rate and increase see the first increment of a series.
	m.fetchedBytes.WithLabelValues(originCache)
	m.fetchedBytes.WithLabelValues(originBucket)
	m.errors.WithLabelValues(operationFetch)
	m.errors.WithLabelValues(operationStore)

	return m
}
