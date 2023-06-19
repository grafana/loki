package cache

import (
	"context"
	"flag"
	"fmt"
	"sync"
	"unsafe"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	lru "github.com/hashicorp/golang-lru/simplelru"
	"github.com/oklog/ulid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/util/flagext"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type codec string

const (
	codecHeaderSnappy             codec = "dvs" // As in "diff+varint+snappy".
	codecHeaderSnappyWithMatchers codec = "dm"  // As in "dvs+matchers"
)

const maxInt = int(^uint(0) >> 1)

const (
	stringHeaderSize = 8
	sliceHeaderSize  = 16
)

var ulidSize = uint64(len(ulid.ULID{}))

type LRUCacheConfig struct {
	MaxSizeBytes     flagext.ByteSize `yaml:"max_size_bytes"`
	MaxItemSizeBytes flagext.ByteSize `yaml:"max_item_size_bytes"`

	MaxItems int `yaml:"max_items"`

	Enabled bool `yaml:"enabled"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *LRUCacheConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.Var(&cfg.MaxItemSizeBytes, prefix+".max-item-size-bytes", description+"Maximum memory size of a single item in the cache. A unit suffix (KB, MB, GB) may be applied.")
	cfg.MaxItemSizeBytes.Set("5MB")

	f.Var(&cfg.MaxSizeBytes, prefix+".max-size-bytes", description+"Maximum memory size of the whole cache. A unit suffix (KB, MB, GB) may be applied.")
	cfg.MaxSizeBytes.Set("500MB")

	f.IntVar(&cfg.MaxItems, prefix+".max-items", 5000, description+"Maximum items in the cache.")
}

func (cfg *LRUCacheConfig) Validate() error {
	return nil
}

type LRUCache struct {
	cacheType stats.CacheType

	done chan struct{}

	mtx sync.Mutex

	logger log.Logger
	lru    *lru.LRU

	maxCacheBytes    uint64
	maxItemSizeBytes uint64
	maxItems         int
	curSize          uint64

	evicted     prometheus.Counter
	requests    prometheus.Counter
	hits        prometheus.Counter
	totalMisses prometheus.Counter
	added       prometheus.Counter
	current     prometheus.Gauge
	bytesInUse  prometheus.Gauge
	overflow    prometheus.Counter
}

func NewLRUCache(name string, cfg LRUCacheConfig, reg prometheus.Registerer, logger log.Logger, cacheType stats.CacheType) (*LRUCache, error) {
	util_log.WarnExperimentalUse(fmt.Sprintf("In-memory (LRU) cache - %s", name), logger)

	c := &LRUCache{
		cacheType: cacheType,

		maxItemSizeBytes: uint64(cfg.MaxItemSizeBytes),
		maxCacheBytes:    uint64(cfg.MaxSizeBytes),
		maxItems:         cfg.MaxItems,

		logger: logger,

		done: make(chan struct{}),
	}

	c.totalMisses = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Namespace:   "loki",
		Subsystem:   "cache",
		Name:        "misses_total",
		Help:        "The total number of Get calls that had no valid entry",
		ConstLabels: prometheus.Labels{"cache": name},
	})

	c.bytesInUse = promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Namespace:   "loki",
		Subsystem:   "cache",
		Name:        "memory_bytes",
		Help:        "The current cache size in bytes",
		ConstLabels: prometheus.Labels{"cache": name},
	})

	c.evicted = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Namespace:   "loki",
		Subsystem:   "cache",
		Name:        "evicted_total",
		Help:        "Total number of items that were evicted.",
		ConstLabels: prometheus.Labels{"cache": name},
	})

	c.added = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Namespace:   "loki",
		Subsystem:   "cache",
		Name:        "added_total",
		Help:        "Total number of items that were added to the cache.",
		ConstLabels: prometheus.Labels{"cache": name},
	})

	c.requests = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Namespace:   "loki",
		Subsystem:   "cache",
		Name:        "gets_total",
		Help:        "Total number of requests to the cache.",
		ConstLabels: prometheus.Labels{"cache": name},
	})

	c.hits = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Namespace:   "loki",
		Subsystem:   "cache",
		Name:        "hits_total",
		Help:        "Total number of requests to the cache that were a hit.",
		ConstLabels: prometheus.Labels{"cache": name},
	})

	c.current = promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Namespace:   "loki",
		Subsystem:   "cache",
		Name:        "entries",
		Help:        "Current number of items in the cache.",
		ConstLabels: prometheus.Labels{"cache": name},
	})

	c.overflow = promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Namespace:   "loki",
		Subsystem:   "cache",
		Name:        "overflow",
		Help:        "Total number of items that could not be added to the cache due to being too big.",
		ConstLabels: prometheus.Labels{"cache": name},
	})

	// Initialize LRU cache with a high size limit since we will manage evictions ourselves
	// based on stored size using `RemoveOldest` method.
	l, err := lru.NewLRU(c.maxItems, c.onEvict)
	if err != nil {
		return nil, err
	}
	c.lru = l

	level.Info(logger).Log(
		"msg", "created in-memory LRU cache",
	)

	return c, nil
}

// Fetch implements Cache.
func (c *LRUCache) Fetch(_ context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	found, missing, bufs = make([]string, 0, len(keys)), make([]string, 0, len(keys)), make([][]byte, 0, len(keys))
	for _, key := range keys {
		val, ok := c.get(key)
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
func (c *LRUCache) Store(_ context.Context, keys []string, values [][]byte) error {
	for i := range keys {
		c.set(keys[i], values[i])
	}

	return nil
}

// Stop implements Cache.
func (c *LRUCache) Stop() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	close(c.done)

	c.reset()
}

func (c *LRUCache) GetCacheType() stats.CacheType {
	return c.cacheType
}

func (c *LRUCache) onEvict(key, val interface{}) {
	c.evicted.Inc()
	c.current.Dec()

	size := entryMemoryUsage(key.(string), val.([]byte))
	c.bytesInUse.Sub(float64(size))
	c.curSize -= uint64(size)
}

func (c *LRUCache) get(key string) ([]byte, bool) {
	c.requests.Inc()

	c.mtx.Lock()
	defer c.mtx.Unlock()

	v, ok := c.lru.Get(key)
	if !ok {
		c.totalMisses.Inc()
		return nil, false
	}
	c.hits.Inc()
	return v.([]byte), true
}

func (c *LRUCache) set(key string, val []byte) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	wasUpdate := false
	var oldValue []byte
	if v, ok := c.lru.Get(key); ok {
		wasUpdate = true
		oldValue = v.([]byte)
	}

	if !c.ensureFits(key, val) {
		c.overflow.Inc()
		return
	}

	// The caller may be passing in a sub-slice of a huge array. Copy the data
	// to ensure we don't waste huge amounts of space for something small.
	v := make([]byte, len(val))
	copy(v, val)
	c.lru.Add(key, v)

	size := entryMemoryUsage(key, val)
	c.bytesInUse.Add(float64(size))
	c.curSize += uint64(size)

	if wasUpdate {
		// it was an update - discount previous value.
		previousSize := entryMemoryUsage(key, oldValue)
		c.bytesInUse.Add(float64(-previousSize))
		c.curSize -= uint64(previousSize)
		return
	}

	c.current.Inc()
	c.added.Inc()
}

func (c *LRUCache) ensureFits(key string, val []byte) bool {
	size := entryMemoryUsage(key, val)
	if size > int(c.maxItemSizeBytes) {
		level.Debug(c.logger).Log(
			"msg", "item bigger than maxItemSizeBytes. Ignoring..",
			"max_item_size_bytes", c.maxItemSizeBytes,
			"max_cache_bytes", c.maxCacheBytes,
			"cur_size", c.curSize,
			"item_size", size,
		)
		return false
	}

	for c.curSize+uint64(size) > c.maxCacheBytes {
		if _, _, ok := c.lru.RemoveOldest(); !ok {
			level.Error(c.logger).Log(
				"msg", "LRU has nothing more to evict, but we still cannot allocate the item. Resetting cache.",
				"max_item_size_bytes", c.maxItemSizeBytes,
				"max_size_bytes", c.maxCacheBytes,
				"cur_size", c.curSize,
				"item_size", size,
			)
			c.reset()
		}
	}
	return true
}

func entryMemoryUsage(key string, val []byte) int {
	if len(val) == 0 {
		return 0
	}
	return int(unsafe.Sizeof(val)) + len(key)
}

func (c *LRUCache) reset() {
	c.lru.Purge()
	c.current.Set(0)
	c.bytesInUse.Set(0)
}
