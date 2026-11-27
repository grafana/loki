package hintprovider

import (
	"fmt"
	"sync"

	"github.com/hashicorp/golang-lru/v2/simplelru"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

const (
	// maxMetadataCacheEntries caps the number of cached indexes, whatever
	// their size. Entries with no size estimate are bounded by it alone.
	maxMetadataCacheEntries = 1000

	// DefaultMetadataCacheMaxBytes is the byte budget of the metadata cache of
	// a component that has no setting for it.
	DefaultMetadataCacheMaxBytes uint64 = 512 << 20
)

// MetadataCacheConfig sizes the cache of parsed index metadata. A nil config,
// or one with a MaxBytes of zero, disables the cache.
type MetadataCacheConfig struct {
	// MaxBytes bounds the estimated size of the cached metadata. Past it, the
	// least recently used entries are evicted.
	MaxBytes uint64
	// Registerer receives the cache metrics. It may be nil.
	Registerer prometheus.Registerer
}

type cachedMetadata struct {
	headerInfo format.HeaderInfo // for size estimation and tracking reader
	state      any               // opaque reader state for fast reopen
}

// metadataCache is a concurrency-safe LRU cache for parsed index metadata. It
// lets the hint provider skip the range reads of an index it opened before.
//
// Entries are keyed by index ID. An index and its meta.json never change once
// written, so an entry cannot go stale. It can only stop being asked for, when
// the index is compacted or deleted, and then it ages out of the LRU.
type metadataCache struct {
	mu       sync.Mutex
	lru      *simplelru.LRU[string, cachedMetadata]
	maxBytes uint64
	bytes    uint64
	metrics  *cacheMetrics
}

func newMetadataCache(maxEntries int, maxBytes uint64, reg prometheus.Registerer) (*metadataCache, error) {
	c := &metadataCache{
		maxBytes: maxBytes,
		metrics:  newCacheMetrics(reg),
	}
	lru, err := simplelru.NewLRU(maxEntries, c.onRemove)
	if err != nil {
		return nil, fmt.Errorf("create metadata cache: %w", err)
	}
	c.lru = lru
	c.syncMetricsLocked()
	return c, nil
}

// onRemove runs under c.mu whenever the LRU drops an entry: an eviction, a
// delete, or a put that replaces an oversized entry.
func (c *metadataCache) onRemove(_ string, value cachedMetadata) {
	c.bytes -= estimateBytes(value)
}

func (c *metadataCache) get(id string) (cachedMetadata, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Get(id)
}

func (c *metadataCache) put(id string, value cachedMetadata) {
	if value.state == nil {
		return
	}
	size := estimateBytes(value)

	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.syncMetricsLocked()

	// An entry larger than the whole budget would evict every other entry and
	// then itself, so it is not cached.
	if size > c.maxBytes {
		c.lru.Remove(id)
		c.metrics.drops.Inc()
		return
	}

	// Add does not call onRemove when it replaces the value of a key.
	if old, ok := c.lru.Peek(id); ok {
		c.bytes -= estimateBytes(old)
	}
	c.bytes += size
	if c.lru.Add(id, value) {
		c.metrics.evictions.Inc()
	}
	// The new entry is the most recent and fits the budget alone, so this
	// stops before removing it.
	for c.bytes > c.maxBytes {
		if _, _, ok := c.lru.RemoveOldest(); !ok {
			break
		}
		c.metrics.evictions.Inc()
	}
}

func (c *metadataCache) delete(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.lru.Remove(id) {
		c.syncMetricsLocked()
	}
}

func (c *metadataCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len()
}

func estimateBytes(value cachedMetadata) uint64 {
	info := value.headerInfo
	return info.DocMetadataSize + info.TermBlockDirSize + info.PostingsBlockDirSize
}

func (c *metadataCache) syncMetricsLocked() {
	c.metrics.entries.Set(float64(c.lru.Len()))
	c.metrics.sizeBytes.Set(float64(c.bytes))
}
