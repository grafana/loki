package hintprovider

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// estimate of the maximum number of index files we expect in a cell
const defaultMetadataCacheEntries = 5000

type cachedMetadata struct {
	headerInfo format.HeaderInfo // for size estimation and tracking reader
	state      any               // opaque reader state for fast reopen
}

// metadataCache is a concurrency-safe cache for parsed index metadata.
// It allows the hint provider to skip GCS range reads for previously-opened
// index files.
type metadataCache struct {
	mu            sync.RWMutex
	entries       map[string]cachedMetadata
	maxEntries    int
	metrics       *cacheMetrics
	estimatedSize uint64
}

func newMetadataCache(maxEntries int, reg prometheus.Registerer) *metadataCache {
	cache := &metadataCache{
		entries:    make(map[string]cachedMetadata),
		maxEntries: maxEntries,
		metrics:    newCacheMetrics(reg),
	}
	cache.syncMetricsLocked()
	return cache
}

func (c *metadataCache) get(id string) (cachedMetadata, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	elem, ok := c.entries[id]
	if !ok {
		return cachedMetadata{}, false
	}
	return elem, true
}

func (c *metadataCache) put(id string, value cachedMetadata) {
	if value.state == nil || c.maxEntries <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.entries[id]; ok {
		c.estimatedSize -= estimateBytes(c.entries[id])
		c.entries[id] = value
		c.estimatedSize += estimateBytes(value)
		c.syncMetricsLocked()
		return
	}
	// Safety cap: once full, do not cache additional entries until stale ones
	// are evicted by snapshot updates.
	if len(c.entries) >= c.maxEntries {
		c.metrics.drops.Inc()
		return
	}
	c.entries[id] = value
	c.estimatedSize += estimateBytes(value)
	c.syncMetricsLocked()
}

func (c *metadataCache) delete(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value, ok := c.entries[id]
	if !ok {
		return
	}
	delete(c.entries, id)
	c.estimatedSize -= estimateBytes(value)
	c.syncMetricsLocked()
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
	for id := range c.entries {
		if _, ok := activeIDs[id]; ok {
			continue
		}
		c.estimatedSize -= estimateBytes(c.entries[id])
		delete(c.entries, id)
		updated = true
	}
	if updated {
		c.syncMetricsLocked()
	}
}

func (c *metadataCache) len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
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
