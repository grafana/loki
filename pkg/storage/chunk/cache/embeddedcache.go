package cache

import (
	"container/list"
	"context"
	"flag"
	"sync"
	"time"
	"unsafe"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
)

const (
	elementSize    = int(unsafe.Sizeof(list.Element{}))
	elementPrtSize = int(unsafe.Sizeof(&list.Element{}))

	defaultPurgeInterval = 1 * time.Minute

	expiredReason string = "expired" //nolint:staticcheck
	fullReason           = "full"
	tooBigReason         = "object too big"
)

// EmbeddedCache is a simple string -> interface{} cache which uses a fifo slide to
// manage evictions.  O(1) inserts and updates, O(1) gets.
//
// This embedded cache implementation supports two eviction methods - based on number of items in the cache, and based on memory usage.
// For the memory-based eviction, set EmbeddedCacheConfig.MaxSizeMB to a positive integer, indicating upper limit of memory allocated by items in the cache.
// Alternatively, set EmbeddedCacheConfig.MaxSizeItems to a positive integer, indicating maximum number of items in the cache.
// If both parameters are set, both methods are enforced, whichever hits first.
type EmbeddedCache struct {
	cacheType stats.CacheType

	lock          sync.RWMutex
	maxSizeItems  int
	maxSizeBytes  uint64
	currSizeBytes uint64

	entries map[string]*list.Element
	lru     *list.List

	done chan struct{}

	entriesAddedNew prometheus.Counter
	entriesEvicted  *prometheus.CounterVec
	entriesCurrent  prometheus.Gauge
	memoryBytes     prometheus.Gauge
}

type cacheEntry struct {
	updated time.Time
	key     string
	value   []byte
}

// EmbeddedCacheConfig represents in-process embedded cache config.
type EmbeddedCacheConfig struct {
	Enabled      bool          `yaml:"enabled,omitempty"`
	MaxSizeMB    int64         `yaml:"max_size_mb"`
	MaxSizeItems int           `yaml:"max_size_items"`
	TTL          time.Duration `yaml:"ttl"`

	// PurgeInterval tell how often should we remove keys that are expired.
	// by default it takes `defaultPurgeInterval`
	PurgeInterval time.Duration `yaml:"-"`
}

func (cfg *EmbeddedCacheConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"embedded-cache.enabled", false, description+"Whether embedded cache is enabled.")
	f.Int64Var(&cfg.MaxSizeMB, prefix+"embedded-cache.max-size-mb", 100, description+"Maximum memory size of the cache in MB.")
	f.IntVar(&cfg.MaxSizeItems, prefix+"embedded-cache.max-size-items", 0, description+"Maximum number of entries in the cache.")
	f.DurationVar(&cfg.TTL, prefix+"embedded-cache.ttl", time.Hour, description+"The time to live for items in the cache before they get purged.")
}

func (cfg *EmbeddedCacheConfig) IsEnabled() bool {
	return cfg.Enabled
}

// NewEmbeddedCache returns a new initialised EmbeddedCache.
func NewEmbeddedCache(name string, cfg EmbeddedCacheConfig, reg prometheus.Registerer, logger log.Logger, cacheType stats.CacheType) *EmbeddedCache {
	if cfg.MaxSizeMB == 0 && cfg.MaxSizeItems == 0 {
		// zero cache capacity - no need to create cache
		level.Warn(logger).Log("msg", "neither embedded-cache.max-size-mb nor embedded-cache.max-size-items is set", "cache", name)
		return nil
	}

	// Set a default interval for the ticker
	// This can be overwritten to a smaller value in tests
	if cfg.PurgeInterval == 0 {
		cfg.PurgeInterval = defaultPurgeInterval
	}

	cache := &EmbeddedCache{
		cacheType: cacheType,

		maxSizeItems: cfg.MaxSizeItems,
		maxSizeBytes: uint64(cfg.MaxSizeMB * 1e6),
		entries:      make(map[string]*list.Element),
		lru:          list.New(),

		done: make(chan struct{}),

		entriesAddedNew: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   "loki",
			Subsystem:   "embeddedcache",
			Name:        "added_new_total",
			Help:        "The total number of new entries added to the cache",
			ConstLabels: prometheus.Labels{"cache": name},
		}),

		entriesEvicted: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace:   "loki",
			Subsystem:   "embeddedcache",
			Name:        "evicted_total",
			Help:        "The total number of evicted entries",
			ConstLabels: prometheus.Labels{"cache": name},
		}, []string{"reason"}),

		entriesCurrent: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace:   "loki",
			Subsystem:   "embeddedcache",
			Name:        "entries",
			Help:        "Current number of entries in the cache",
			ConstLabels: prometheus.Labels{"cache": name},
		}),

		memoryBytes: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace:   "loki",
			Subsystem:   "embeddedcache",
			Name:        "memory_bytes",
			Help:        "The current cache size in bytes",
			ConstLabels: prometheus.Labels{"cache": name},
		}),
	}

	if cfg.TTL > 0 {
		go cache.runPruneJob(cfg.PurgeInterval, cfg.TTL)
	}

	return cache
}

func (c *EmbeddedCache) runPruneJob(interval, ttl time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.pruneExpiredItems(ttl)
		}
	}
}

// pruneExpiredItems prunes items in the cache that exceeded their ttl
func (c *EmbeddedCache) pruneExpiredItems(ttl time.Duration) {
	c.lock.Lock()
	defer c.lock.Unlock()

	for k, v := range c.entries {
		entry := v.Value.(*cacheEntry)
		if time.Since(entry.updated) > ttl {
			_ = c.lru.Remove(v).(*cacheEntry)
			delete(c.entries, k)
			c.currSizeBytes -= sizeOf(entry)
			c.entriesCurrent.Dec()
			c.entriesEvicted.WithLabelValues(expiredReason).Inc()
		}
	}
}

// Fetch implements Cache.
func (c *EmbeddedCache) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	found, missing, bufs = make([]string, 0, len(keys)), make([]string, 0, len(keys)), make([][]byte, 0, len(keys))
	for _, key := range keys {
		val, ok := c.Get(ctx, key)
		if !ok {
			missing = append(missing, key)
			continue
		}

		found = append(found, key)
		bufs = append(bufs, val)
	}
	return
}

// Store implements Cache.
func (c *EmbeddedCache) Store(_ context.Context, keys []string, values [][]byte) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	for i := range keys {
		c.put(keys[i], values[i])
	}
	return nil
}

// Stop implements Cache.
func (c *EmbeddedCache) Stop() {
	c.lock.Lock()
	defer c.lock.Unlock()

	close(c.done)

	c.entries = make(map[string]*list.Element)
	c.lru.Init()
	c.currSizeBytes = 0

	c.entriesCurrent.Set(float64(0))
	c.memoryBytes.Set(float64(0))
}

func (c *EmbeddedCache) GetCacheType() stats.CacheType {
	return c.cacheType
}

func (c *EmbeddedCache) put(key string, value []byte) {
	// See if we already have the item in the cache.
	element, ok := c.entries[key]
	if ok {
		// Remove the item from the cache.
		entry := c.lru.Remove(element).(*cacheEntry)
		delete(c.entries, key)
		c.currSizeBytes -= sizeOf(entry)
		c.entriesCurrent.Dec()
	}

	entry := &cacheEntry{
		updated: time.Now(),
		key:     key,
		value:   value,
	}
	entrySz := sizeOf(entry)

	if c.maxSizeBytes > 0 && entrySz > c.maxSizeBytes {
		// Cannot keep this item in the cache.
		if ok {
			// We do not replace this item.
			c.entriesEvicted.WithLabelValues(tooBigReason).Inc()
		}
		c.memoryBytes.Set(float64(c.currSizeBytes))
		return
	}

	// Otherwise, see if we need to evict item(s).
	for (c.maxSizeBytes > 0 && c.currSizeBytes+entrySz > c.maxSizeBytes) || (c.maxSizeItems > 0 && len(c.entries) >= c.maxSizeItems) {
		lastElement := c.lru.Back()
		if lastElement == nil {
			break
		}
		evicted := c.lru.Remove(lastElement).(*cacheEntry)
		delete(c.entries, evicted.key)
		c.currSizeBytes -= sizeOf(evicted)
		c.entriesCurrent.Dec()
		c.entriesEvicted.WithLabelValues(fullReason).Inc()
	}

	// Finally, we have space to add the item.
	c.entries[key] = c.lru.PushFront(entry)
	c.currSizeBytes += entrySz
	if !ok {
		c.entriesAddedNew.Inc()
	}
	c.entriesCurrent.Inc()
	c.memoryBytes.Set(float64(c.currSizeBytes))
}

// Get returns the stored value against the key and when the key was last updated.
func (c *EmbeddedCache) Get(_ context.Context, key string) ([]byte, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	element, ok := c.entries[key]
	if ok {
		entry := element.Value.(*cacheEntry)
		return entry.value, true
	}

	return nil, false
}

func sizeOf(item *cacheEntry) uint64 {
	return uint64(int(unsafe.Sizeof(*item)) + // size of cacheEntry
		len(item.key) + // size of key
		cap(item.value) + // size of value
		elementSize + // size of the element in linked list
		elementPrtSize) // size of the pointer to an element in the map
}
