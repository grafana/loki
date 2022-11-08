package cache

import (
	"container/list"
	"context"
	"flag"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	elementSize    = int(unsafe.Sizeof(list.Element{}))
	elementPrtSize = int(unsafe.Sizeof(&list.Element{}))
)

// This FIFO cache implementation supports two eviction methods - based on number of items in the cache, and based on memory usage.
// For the memory-based eviction, set FifoCacheConfig.MaxSizeBytes to a positive integer, indicating upper limit of memory allocated by items in the cache.
// Alternatively, set FifoCacheConfig.MaxSizeItems to a positive integer, indicating maximum number of items in the cache.
// If both parameters are set, both methods are enforced, whichever hits first.

// FifoCacheConfig holds config for the FifoCache.
type FifoCacheConfig struct {
	MaxSizeBytes string        `yaml:"max_size_bytes"`
	MaxSizeItems int           `yaml:"max_size_items"` // deprecated
	TTL          time.Duration `yaml:"ttl"`

	DeprecatedValidity time.Duration `yaml:"validity"`
	DeprecatedSize     int           `yaml:"size"`

	PurgeInterval time.Duration
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *FifoCacheConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.StringVar(&cfg.MaxSizeBytes, prefix+"fifocache.max-size-bytes", "1GB", description+"Maximum memory size of the cache in bytes. A unit suffix (KB, MB, GB) may be applied.")
	f.IntVar(&cfg.MaxSizeItems, prefix+"fifocache.max-size-items", 0, description+"deprecated: Maximum number of entries in the cache.")
	f.DurationVar(&cfg.TTL, prefix+"fifocache.ttl", time.Hour, description+"The time to live for items in the cache before they get purged.")

	f.DurationVar(&cfg.DeprecatedValidity, prefix+"fifocache.duration", 0, "Deprecated (use ttl instead): "+description+"The expiry duration for the cache.")
	f.IntVar(&cfg.DeprecatedSize, prefix+"fifocache.size", 0, "Deprecated (use max-size-items or max-size-bytes instead): "+description+"The number of entries to cache.")
}

func (cfg *FifoCacheConfig) Validate() error {
	_, err := parsebytes(cfg.MaxSizeBytes)
	return err
}

func parsebytes(s string) (uint64, error) {
	if len(s) == 0 {
		return 0, nil
	}
	bytes, err := humanize.ParseBytes(s)
	if err != nil {
		return 0, errors.Wrap(err, "invalid FifoCache config")
	}
	return bytes, nil
}

// FifoCache is a simple string -> interface{} cache which uses a fifo slide to
// manage evictions.  O(1) inserts and updates, O(1) gets.
type FifoCache struct {
	cacheType stats.CacheType

	lock          sync.RWMutex
	maxSizeItems  int
	maxSizeBytes  uint64
	currSizeBytes uint64

	entries map[string]*list.Element
	lru     *list.List

	done chan struct{}

	entriesAdded    prometheus.Counter
	entriesAddedNew prometheus.Counter
	entriesEvicted  *prometheus.CounterVec
	entriesCurrent  prometheus.Gauge
	totalGets       prometheus.Counter
	totalMisses     prometheus.Counter
	staleGets       prometheus.Counter
	memoryBytes     prometheus.Gauge
}

const (
	expiredReason string = "expired" //nolint:staticcheck
	fullReason           = "full"
	tooBigReason         = "object too big"
)

type cacheEntry struct {
	updated time.Time
	key     string
	value   []byte
}

// NewFifoCache returns a new initialised FifoCache of size.
func NewFifoCache(name string, cfg FifoCacheConfig, reg prometheus.Registerer, logger log.Logger, cacheType stats.CacheType) *FifoCache {
	util_log.WarnExperimentalUse(fmt.Sprintf("In-memory (FIFO) cache - %s", name), logger)

	if cfg.DeprecatedSize > 0 {
		flagext.DeprecatedFlagsUsed.Inc()
		level.Warn(logger).Log("msg", "running with DEPRECATED flag fifocache.size, use fifocache.max-size-items or fifocache.max-size-bytes instead", "cache", name)
		cfg.MaxSizeItems = cfg.DeprecatedSize
	}
	maxSizeBytes, _ := parsebytes(cfg.MaxSizeBytes)

	if maxSizeBytes == 0 && cfg.MaxSizeItems == 0 {
		// zero cache capacity - no need to create cache
		level.Warn(logger).Log("msg", "neither fifocache.max-size-bytes nor fifocache.max-size-items is set", "cache", name)
		return nil
	}

	if cfg.DeprecatedValidity > 0 {
		flagext.DeprecatedFlagsUsed.Inc()
		level.Warn(logger).Log("msg", "running with DEPRECATED flag fifocache.duration, use fifocache.ttl instead", "cache", name)
		cfg.TTL = cfg.DeprecatedValidity
	}

	// Set a default interval for the ticker
	// This can be overwritten to a smaller value in tests
	if cfg.PurgeInterval == 0 {
		cfg.PurgeInterval = 1 * time.Minute
	}

	cache := &FifoCache{
		cacheType: cacheType,

		maxSizeItems: cfg.MaxSizeItems,
		maxSizeBytes: maxSizeBytes,
		entries:      make(map[string]*list.Element),
		lru:          list.New(),

		done: make(chan struct{}),

		entriesAdded: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   "querier",
			Subsystem:   "cache",
			Name:        "added_total",
			Help:        "The total number of Put calls on the cache",
			ConstLabels: prometheus.Labels{"cache": name},
		}),

		entriesAddedNew: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   "querier",
			Subsystem:   "cache",
			Name:        "added_new_total",
			Help:        "The total number of new entries added to the cache",
			ConstLabels: prometheus.Labels{"cache": name},
		}),

		entriesEvicted: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace:   "querier",
			Subsystem:   "cache",
			Name:        "evicted_total",
			Help:        "The total number of evicted entries",
			ConstLabels: prometheus.Labels{"cache": name},
		}, []string{"reason"}),

		entriesCurrent: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace:   "querier",
			Subsystem:   "cache",
			Name:        "entries",
			Help:        "The total number of entries",
			ConstLabels: prometheus.Labels{"cache": name},
		}),

		totalGets: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   "querier",
			Subsystem:   "cache",
			Name:        "gets_total",
			Help:        "The total number of Get calls",
			ConstLabels: prometheus.Labels{"cache": name},
		}),

		totalMisses: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   "querier",
			Subsystem:   "cache",
			Name:        "misses_total",
			Help:        "The total number of Get calls that had no valid entry",
			ConstLabels: prometheus.Labels{"cache": name},
		}),

		staleGets: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   "querier",
			Subsystem:   "cache",
			Name:        "stale_gets_total",
			Help:        "The total number of Get calls that had an entry which expired (deprecated)",
			ConstLabels: prometheus.Labels{"cache": name},
		}),

		memoryBytes: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace:   "querier",
			Subsystem:   "cache",
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

func (c *FifoCache) runPruneJob(interval, ttl time.Duration) {
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
func (c *FifoCache) pruneExpiredItems(ttl time.Duration) {
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
func (c *FifoCache) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
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
func (c *FifoCache) Store(ctx context.Context, keys []string, values [][]byte) error {
	c.entriesAdded.Inc()

	c.lock.Lock()
	defer c.lock.Unlock()

	for i := range keys {
		c.put(keys[i], values[i])
	}
	return nil
}

// Stop implements Cache.
func (c *FifoCache) Stop() {
	c.lock.Lock()
	defer c.lock.Unlock()

	close(c.done)

	c.entries = make(map[string]*list.Element)
	c.lru.Init()
	c.currSizeBytes = 0

	c.entriesCurrent.Set(float64(0))
	c.memoryBytes.Set(float64(0))
}

func (c *FifoCache) GetCacheType() stats.CacheType {
	return c.cacheType
}

func (c *FifoCache) put(key string, value []byte) {
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
func (c *FifoCache) Get(ctx context.Context, key string) ([]byte, bool) {
	c.totalGets.Inc()

	c.lock.RLock()
	defer c.lock.RUnlock()

	element, ok := c.entries[key]
	if ok {
		entry := element.Value.(*cacheEntry)
		return entry.value, true
	}

	c.totalMisses.Inc()
	return nil, false
}

func sizeOf(item *cacheEntry) uint64 {
	return uint64(int(unsafe.Sizeof(*item)) + // size of cacheEntry
		len(item.key) + // size of key
		cap(item.value) + // size of value
		elementSize + // size of the element in linked list
		elementPrtSize) // size of the pointer to an element in the map
}
