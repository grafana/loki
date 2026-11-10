package metadatacache

import (
	"context"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sync/singleflight"
)

// memoryMaxItemBytes is the largest metadata block MemoryCache keeps for one object. It bounds the
// allocation when an object layout is corrupt.
const memoryMaxItemBytes = 64 << 20

// MemoryCache is an in-memory metadata cache. It never evicts and has no limit on its total size.
//
// It keeps the metadata block of every object it loads, or the whole object if it is tiny, for the
// life of the process. Memory use grows with the number of distinct objects read. Do not use it where
// memory is limited.
//
// It asks for the whole metadata block, so a warm open finds the metadata of every section in
// memory, logs sections included.
//
// It is safe for concurrent use.
type MemoryCache struct {
	mu    sync.RWMutex
	items map[string][]byte
	size  int64

	sf singleflight.Group

	hits    prometheus.Counter
	misses  prometheus.Counter
	entries prometheus.Gauge
	bytes   prometheus.Gauge
}

// NewMemoryCache returns an empty MemoryCache. It counts its hits and misses under kind in m, and it
// registers its size gauges on reg. reg may be nil.
//
// It panics if reg already holds the gauges of a MemoryCache of the same kind.
func NewMemoryCache(kind string, m *Metrics, reg prometheus.Registerer) *MemoryCache {
	labels := prometheus.Labels{"kind": kind}
	return &MemoryCache{
		items:  make(map[string][]byte),
		hits:   m.hits.WithLabelValues(kind),
		misses: m.misses.WithLabelValues(kind),
		entries: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name:        "loki_dataobj_metadata_memory_cache_entries",
			Help:        "Number of objects the in-memory data-object metadata cache holds a metadata region for.",
			ConstLabels: labels,
		}),
		bytes: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name:        "loki_dataobj_metadata_memory_cache_bytes",
			Help:        "Total size in bytes of the metadata regions the in-memory data-object metadata cache holds.",
			ConstLabels: labels,
		}),
	}
}

// GetOrLoadMetadataRegion returns the metadata region for key. On a miss it calls load and returns the
// result. It keeps the result for the life of the process, unless the result is empty or larger than
// MaxItemBytes. A failed load is not kept, so the next call loads again.
//
// Concurrent misses for the same key share a single load. It runs on a context that keeps the values
// of the first caller's context but drops its cancellation. A timeout bounds the load.
//
// A caller whose context ends returns that context error. The load keeps running for the other
// callers. A miss on a context that is already done returns its error and starts no load.
//
// The returned slice is read-only and shared between callers.
func (c *MemoryCache) GetOrLoadMetadataRegion(ctx context.Context, key string, load func(context.Context) ([]byte, error)) ([]byte, error) {
	if md, ok := c.get(key); ok {
		c.hits.Inc()
		return md, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.misses.Inc()

	ch := c.sf.DoChan(key, func() (any, error) {
		// Another flight can finish between the lookup above and the start of this one. Check again
		// before loading.
		if md, ok := c.get(key); ok {
			return md, nil
		}

		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loadTimeout)
		defer cancel()

		md, err := load(loadCtx)
		if err != nil {
			return nil, err
		}
		c.put(key, md)
		return md, nil
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.([]byte), nil
	}
}

// MaxItemBytes returns the largest region the cache keeps for one object. An object with a larger
// block is read directly and not kept.
func (c *MemoryCache) MaxItemBytes() int64 { return memoryMaxItemBytes }

// WholeMetadataBlock reports true: the cache asks for the whole metadata block of each object.
func (c *MemoryCache) WholeMetadataBlock() bool { return true }

// Len returns the number of objects the cache holds.
func (c *MemoryCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Bytes returns the total size of the regions the cache holds.
func (c *MemoryCache) Bytes() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.size
}

func (c *MemoryCache) get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	md, ok := c.items[key]
	return md, ok
}

// put keeps md under key. It skips an empty block, which no reader can decode, and a block above
// MaxItemBytes.
func (c *MemoryCache) put(key string, md []byte) {
	if len(md) == 0 || len(md) > memoryMaxItemBytes {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.items[key]; ok {
		c.size -= int64(len(old))
	}
	c.items[key] = md
	c.size += int64(len(md))
	c.entries.Set(float64(len(c.items)))
	c.bytes.Set(float64(c.size))
}
