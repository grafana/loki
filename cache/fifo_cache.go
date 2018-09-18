package cache

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	cacheEntriesAdded = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "querier",
		Subsystem: "cache",
		Name:      "added_total",
		Help:      "The total number of Put calls on the cache",
	}, []string{"cache"})

	cacheEntriesAddedNew = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "querier",
		Subsystem: "cache",
		Name:      "added_new_total",
		Help:      "The total number of new entries added to the cache",
	}, []string{"cache"})

	cacheEntriesEvicted = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "querier",
		Subsystem: "cache",
		Name:      "evicted_total",
		Help:      "The total number of evicted entries",
	}, []string{"cache"})

	cacheTotalGets = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "querier",
		Subsystem: "cache",
		Name:      "gets_total",
		Help:      "The total number of Get calls",
	}, []string{"cache"})

	cacheTotalMisses = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "querier",
		Subsystem: "cache",
		Name:      "misses_total",
		Help:      "The total number of Get calls that had no valid entry",
	}, []string{"cache"})

	cacheStaleGets = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "querier",
		Subsystem: "cache",
		Name:      "stale_gets_total",
		Help:      "The total number of Get calls that had an entry which expired",
	}, []string{"cache"})
)

// FifoCache is a simple string -> interface{} cache which uses a fifo slide to
// manage evictions.  O(1) inserts and updates, O(1) gets.
type FifoCache struct {
	lock     sync.RWMutex
	size     int
	validity time.Duration
	entries  []cacheEntry
	index    map[string]int

	// indexes into entries to identify the most recent and least recent entry.
	first, last int

	name            string
	entriesAdded    prometheus.Counter
	entriesAddedNew prometheus.Counter
	entriesEvicted  prometheus.Counter
	totalGets       prometheus.Counter
	totalMisses     prometheus.Counter
	staleGets       prometheus.Counter
}

type cacheEntry struct {
	updated    time.Time
	key        string
	value      interface{}
	prev, next int
}

// NewFifoCache returns a new initialised FifoCache of size.
func NewFifoCache(name string, size int, validity time.Duration) *FifoCache {
	return &FifoCache{
		size:     size,
		validity: validity,
		entries:  make([]cacheEntry, 0, size),
		index:    make(map[string]int, size),

		name:            name,
		entriesAdded:    cacheEntriesAdded.WithLabelValues(name),
		entriesAddedNew: cacheEntriesAddedNew.WithLabelValues(name),
		entriesEvicted:  cacheEntriesEvicted.WithLabelValues(name),
		totalGets:       cacheTotalGets.WithLabelValues(name),
		totalMisses:     cacheTotalMisses.WithLabelValues(name),
		staleGets:       cacheStaleGets.WithLabelValues(name),
	}
}

// Fetch implements Cache.
func (c *FifoCache) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string) {
	found, missing, bufs = make([]string, 0, len(keys)), make([]string, 0, len(keys)), make([][]byte, 0, len(keys))
	for _, key := range keys {
		val, ok := c.Get(ctx, key)
		if !ok {
			missing = append(missing, key)
			continue
		}

		found = append(found, key)
		bufs = append(bufs, val.([]byte))
	}

	return
}

// Store implements Cache.
func (c *FifoCache) Store(ctx context.Context, keys []string, bufs [][]byte) {
	values := make([]interface{}, 0, len(bufs))
	for _, buf := range bufs {
		values = append(values, buf)
	}
	c.Put(ctx, keys, values)
}

// Stop implements Cache.
func (c *FifoCache) Stop() error {
	return nil
}

// Put stores the value against the key.
func (c *FifoCache) Put(ctx context.Context, keys []string, values []interface{}) {
	c.entriesAdded.Inc()
	if c.size == 0 {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	for i := range keys {
		c.put(ctx, keys[i], values[i])
	}
}

func (c *FifoCache) put(ctx context.Context, key string, value interface{}) {
	// See if we already have the entry
	index, ok := c.index[key]
	if ok {
		entry := c.entries[index]

		entry.updated = time.Now()
		entry.value = value

		// Remove this entry from the FIFO linked-list.
		c.entries[entry.prev].next = entry.next
		c.entries[entry.next].prev = entry.prev

		// Insert it at the beginning
		entry.next = c.first
		entry.prev = c.last
		c.entries[entry.next].prev = index
		c.entries[entry.prev].next = index
		c.first = index

		c.entries[index] = entry
		return
	}
	c.entriesAddedNew.Inc()

	// Otherwise, see if we need to evict an entry.
	if len(c.entries) >= c.size {
		c.entriesEvicted.Inc()
		index = c.last
		entry := c.entries[index]

		c.last = entry.prev
		c.first = index
		delete(c.index, entry.key)
		c.index[key] = index

		entry.updated = time.Now()
		entry.value = value
		entry.key = key
		c.entries[index] = entry
		return
	}

	// Finally, no hit and we have space.
	index = len(c.entries)
	c.entries = append(c.entries, cacheEntry{
		updated: time.Now(),
		key:     key,
		value:   value,
		prev:    c.last,
		next:    c.first,
	})
	c.entries[c.first].prev = index
	c.entries[c.last].next = index
	c.first = index
	c.index[key] = index
}

// Get returns the stored value against the key and when the key was last updated.
func (c *FifoCache) Get(ctx context.Context, key string) (interface{}, bool) {
	c.totalGets.Inc()
	if c.size == 0 {
		return nil, false
	}

	c.lock.RLock()
	defer c.lock.RUnlock()

	index, ok := c.index[key]
	if ok {
		updated := c.entries[index].updated
		if time.Now().Sub(updated) < c.validity {

			return c.entries[index].value, true
		}

		c.totalMisses.Inc()
		c.staleGets.Inc()
		return nil, false
	}

	c.totalMisses.Inc()
	return nil, false
}
