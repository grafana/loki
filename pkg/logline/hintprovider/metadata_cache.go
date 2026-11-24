package hintprovider

import (
	"container/list"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// estimate of the maximum number of index files we expect in a cell
const defaultMetadataCacheEntries = 1000

type cachedMetadata struct {
	headerInfo format.HeaderInfo // for size estimation and tracking reader
	state      any               // opaque reader state for fast reopen
}

// DefaultMetadataCacheMaxBytes is the default byte budget of the metadata cache.
const DefaultMetadataCacheMaxBytes uint64 = 2 << 30

type cacheItem struct {
	id    string
	value cachedMetadata
	size  uint64
}

// metadataCache is a concurrency-safe LRU cache for parsed index metadata.
// It allows the hint provider to skip GCS range reads for previously-opened
// index files. It is bounded by entry count and by estimated bytes; inserts
// that would exceed either limit evict the least recently used entries.
type metadataCache struct {
	mu            sync.Mutex
	entries       map[string]*list.Element // values are *cacheItem
	lru           *list.List               // front = most recently used
	maxEntries    int
	maxBytes      uint64 // 0 means no byte budget
	metrics       *cacheMetrics
	estimatedSize uint64
}

func newMetadataCache(maxEntries int, maxBytes uint64, reg prometheus.Registerer) *metadataCache {
	cache := &metadataCache{
		entries:    make(map[string]*list.Element),
		lru:        list.New(),
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
		metrics:    newCacheMetrics(reg),
	}
	cache.syncMetricsLocked()
	return cache
}

// get returns the entry for id and marks it as recently used.
func (c *metadataCache) get(id string) (cachedMetadata, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.entries[id]
	if !ok {
		return cachedMetadata{}, false
	}
	c.lru.MoveToFront(elem)
	return elem.Value.(*cacheItem).value, true
}

func (c *metadataCache) put(id string, value cachedMetadata) {
	if value.state == nil || c.maxEntries <= 0 {
		return
	}
	size := estimateBytes(value)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.maxBytes > 0 && size > c.maxBytes {
		// Larger than the whole budget: caching it would evict everything
		// and still not fit. Drop any older entry for the key as it is stale.
		if elem, ok := c.entries[id]; ok {
			c.removeLocked(elem)
		}
		c.metrics.drops.Inc()
		c.syncMetricsLocked()
		return
	}

	if elem, ok := c.entries[id]; ok {
		item := elem.Value.(*cacheItem)
		c.estimatedSize -= item.size
		item.value, item.size = value, size
		c.estimatedSize += size
		c.lru.MoveToFront(elem)
	} else {
		c.entries[id] = c.lru.PushFront(&cacheItem{id: id, value: value, size: size})
		c.estimatedSize += size
	}

	// Evict least recently used entries until within limits. The entry just
	// inserted is at the front and fits the budget on its own, so it survives.
	for c.lru.Len() > 1 && (c.lru.Len() > c.maxEntries || (c.maxBytes > 0 && c.estimatedSize > c.maxBytes)) {
		c.removeLocked(c.lru.Back())
		c.metrics.evictions.Inc()
	}
	c.syncMetricsLocked()
}

func (c *metadataCache) delete(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.entries[id]
	if !ok {
		return
	}
	c.removeLocked(elem)
	c.syncMetricsLocked()
}

// removeLocked removes elem and keeps estimatedSize in sync. It does not
// update metrics.
func (c *metadataCache) removeLocked(elem *list.Element) {
	item := c.lru.Remove(elem).(*cacheItem)
	delete(c.entries, item.id)
	c.estimatedSize -= item.size
}

// evictStale removes entries that no longer appear in the active snapshot,
// preventing the cache from holding references to deleted or compacted indexes.
func (c *metadataCache) evictStale(snap *store.Snapshot) {
	if snap == nil {
		return
	}

	active := snap.Active()
	activeIDs := make(map[string]struct{}, len(active))
	for _, meta := range active {
		activeIDs[meta.ID()] = struct{}{}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	updated := false
	for id, elem := range c.entries {
		if _, ok := activeIDs[id]; ok {
			continue
		}
		c.removeLocked(elem)
		updated = true
	}
	if updated {
		c.syncMetricsLocked()
	}
}

func (c *metadataCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func estimateBytes(value cachedMetadata) uint64 {
	info := value.headerInfo
	return info.DocMetadataSize + info.TermBlockDirSize + info.PostingsBlockDirSize
}

func (c *metadataCache) syncMetricsLocked() {
	if c.metrics == nil {
		return
	}
	c.metrics.entries.Set(float64(len(c.entries)))
	c.metrics.sizeBytes.Set(float64(c.estimatedSize))
}
