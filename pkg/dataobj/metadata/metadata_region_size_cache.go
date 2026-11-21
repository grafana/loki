package metadata

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/simplelru"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	metadataRegionSizeCacheIdleTimeout   = 24 * time.Hour
	metadataRegionSizeCachePurgeInterval = time.Minute

	// metadataRegionSizeEntryOverheadBytes estimates the memory of an entry on top of its path: the LRU
	// node, the map slot and the metadataRegionSizeEntry. A measurement on 64-bit platforms gave 147
	// bytes. The constant rounds it up.
	metadataRegionSizeEntryOverheadBytes = 160
)

// Values of the result label of the lookups counter.
const (
	lookupHit  = "hit"
	lookupMiss = "miss"
)

// Values of the reason label of the evictions counter.
const (
	evictionSize = "size"
	evictionIdle = "idle"
)

// metadataRegionSizeCache remembers the size of the metadata region of data objects, by object path.
// An object never changes, so an entry never goes stale.
//
// The cache holds at most maxBytes, by an estimate of the memory of each entry. A get that finds an
// entry, and a set of a known path, count as a use of the entry. When the cache is full, it drops the
// entry that was used least recently. It also drops the entries that were not used for
// metadataRegionSizeCacheIdleTimeout. That drop runs from get and set, at most once every
// metadataRegionSizeCachePurgeInterval. So a cache that nobody uses keeps its entries.
//
// It is safe for concurrent use. A nil *metadataRegionSizeCache remembers nothing.
type metadataRegionSizeCache struct {
	maxBytes int64
	now      func() time.Time

	mu           sync.Mutex
	entries      *simplelru.LRU[string, *metadataRegionSizeEntry]
	entriesBytes int64
	lastPurge    time.Time

	hits, misses                 prometheus.Counter
	evictedBySize, evictedByIdle prometheus.Counter
}

type metadataRegionSizeEntry struct {
	size   int64
	usedAt time.Time
}

// newMetadataRegionSizeCache returns a cache that holds at most maxBytes, or nil when maxBytes is not
// positive. It registers its metrics on reg, which may be nil.
func newMetadataRegionSizeCache(maxBytes int64, now func() time.Time, reg prometheus.Registerer) *metadataRegionSizeCache {
	if maxBytes <= 0 {
		return nil
	}

	lookups := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "loki_dataobj_metadata_prefetcher_region_size_cache_lookups_total",
		Help: "Lookups in the in-memory metadata region size cache, by result: hit or miss.",
	}, []string{"result"})
	evictions := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "loki_dataobj_metadata_prefetcher_region_size_cache_evictions_total",
		Help: "Entries that the in-memory metadata region size cache dropped, by reason: size when the cache was full, idle when nothing used the entry for 24 hours.",
	}, []string{"reason"})

	r := &metadataRegionSizeCache{
		maxBytes:      maxBytes,
		now:           now,
		hits:          lookups.WithLabelValues(lookupHit),
		misses:        lookups.WithLabelValues(lookupMiss),
		evictedBySize: evictions.WithLabelValues(evictionSize),
		evictedByIdle: evictions.WithLabelValues(evictionIdle),
	}

	// maxBytes bounds the cache, so the LRU needs no limit on the entry count. The callback keeps the
	// byte count right for every removal.
	entries, err := simplelru.NewLRU(math.MaxInt, func(path string, _ *metadataRegionSizeEntry) {
		r.entriesBytes -= entryBytes(path)
	})
	if err != nil {
		// NewLRU fails only for a size of 0 or less.
		panic(fmt.Errorf("creating the region size LRU: %w", err))
	}
	r.entries = entries

	promauto.With(reg).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "loki_dataobj_metadata_prefetcher_region_size_cache_entries",
		Help: "Number of objects in the in-memory metadata region size cache.",
	}, func() float64 {
		r.mu.Lock()
		defer r.mu.Unlock()
		return float64(r.entries.Len())
	})
	promauto.With(reg).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "loki_dataobj_metadata_prefetcher_region_size_cache_bytes",
		Help: "Estimated memory in bytes of the in-memory metadata region size cache.",
	}, func() float64 {
		r.mu.Lock()
		defer r.mu.Unlock()
		return float64(r.entriesBytes)
	})
	return r
}

// get returns the size of the region of the object at path. A hit counts as a use of the entry.
func (r *metadataRegionSizeCache) get(path string) (size int64, ok bool) {
	if r == nil {
		return 0, false
	}

	// The deferred calls run in reverse order. So the unlock runs first, and the counters stay out of
	// the critical section.
	var idle int
	defer func() {
		r.evictedByIdle.Add(float64(idle))
		if ok {
			r.hits.Inc()
		} else {
			r.misses.Inc()
		}
	}()

	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	idle = r.purgeIdleLocked(now)

	e, ok := r.entries.Get(path)
	if !ok {
		return 0, false
	}
	e.usedAt = now
	return e.size, true
}

// set remembers the size of the region of the object at path. It ignores a size that is not positive,
// and an entry that is larger than the whole cache. A set of a known path counts as a use of the
// entry.
func (r *metadataRegionSizeCache) set(path string, size int64) {
	if r == nil || size <= 0 || entryBytes(path) > r.maxBytes {
		return
	}

	// The counters stay out of the critical section, as in get.
	var idle, full int
	defer func() {
		r.evictedByIdle.Add(float64(idle))
		r.evictedBySize.Add(float64(full))
	}()

	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	idle = r.purgeIdleLocked(now)

	if e, ok := r.entries.Get(path); ok {
		e.size, e.usedAt = size, now
		return
	}

	// The cache outlives the request. The clone makes it keep only the path, and not a larger string
	// that the path could alias.
	path = strings.Clone(path)
	r.entries.Add(path, &metadataRegionSizeEntry{size: size, usedAt: now})
	r.entriesBytes += entryBytes(path)
	for r.entriesBytes > r.maxBytes {
		if _, _, ok := r.entries.RemoveOldest(); !ok {
			return
		}
		full++
	}
}

// purgeIdleLocked drops the entries that were not used for metadataRegionSizeCacheIdleTimeout. It
// returns the number of entries that it dropped. The LRU holds the entries in order of last use, so the
// purge stops at the first entry that is not idle.
func (r *metadataRegionSizeCache) purgeIdleLocked(now time.Time) (dropped int) {
	if now.Sub(r.lastPurge) < metadataRegionSizeCachePurgeInterval {
		return 0
	}
	r.lastPurge = now

	for {
		_, oldest, ok := r.entries.GetOldest()
		if !ok || now.Sub(oldest.usedAt) < metadataRegionSizeCacheIdleTimeout {
			return dropped
		}
		r.entries.RemoveOldest()
		dropped++
	}
}

func entryBytes(path string) int64 {
	return int64(len(path)) + metadataRegionSizeEntryOverheadBytes
}
