package cache

import (
	"context"
	"flag"
	"fmt"
	"sync"
	"unsafe"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	util_log "github.com/grafana/loki/pkg/util/log"
	lru "github.com/hashicorp/golang-lru/simplelru"
	"github.com/oklog/ulid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
	MaxSizeBytes string `yaml:"max_size_bytes"`

	Enabled bool `yaml:"enabled"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *LRUCacheConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.StringVar(&cfg.MaxSizeBytes, prefix+"lrucache.max-size-bytes", "500MB", description+"Maximum memory size of the cache in bytes. A unit suffix (KB, MB, GB) may be applied.")
}

func (cfg *LRUCacheConfig) Validate() error {
	_, err := parsebytes(cfg.MaxSizeBytes)
	return err
}

type LRUCache struct {
	cacheType stats.CacheType

	done chan struct{}

	mtx sync.Mutex

	logger           log.Logger
	lru              *lru.LRU
	maxSizeBytes     uint64
	maxItemSizeBytes uint64

	evicted     *prometheus.CounterVec
	requests    *prometheus.CounterVec
	hits        *prometheus.CounterVec
	totalMisses prometheus.Counter
	added       *prometheus.CounterVec
	current     *prometheus.GaugeVec
	bytesInUse  prometheus.Gauge
}

func NewLRUCache(name string, cfg LRUCacheConfig, reg prometheus.Registerer, logger log.Logger, cacheType stats.CacheType) (*LRUCache, error) {
	util_log.WarnExperimentalUse(fmt.Sprintf("In-memory (LRU) cache - %s", name), logger)

	maxSizeBytes, _ := parsebytes(cfg.MaxSizeBytes)

	c := &LRUCache{
		cacheType: cacheType,

		maxSizeBytes: maxSizeBytes,
		logger:       logger,

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

	c.evicted = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Namespace:   "loki",
		Subsystem:   "cache",
		Name:        "evicted_total",
		Help:        "Total number of items that were evicted.",
		ConstLabels: prometheus.Labels{"cache": name},
	}, []string{})

	c.added = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Namespace:   "loki",
		Subsystem:   "cache",
		Name:        "added_total",
		Help:        "Total number of items that were added to the cache.",
		ConstLabels: prometheus.Labels{"cache": name},
	}, []string{})

	c.requests = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Namespace:   "loki",
		Subsystem:   "cache",
		Name:        "gets_total",
		Help:        "Total number of requests to the cache.",
		ConstLabels: prometheus.Labels{"cache": name},
	}, []string{})

	c.hits = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Namespace:   "loki",
		Subsystem:   "cache",
		Name:        "hits_total",
		Help:        "Total number of requests to the cache that were a hit.",
		ConstLabels: prometheus.Labels{"cache": name},
	}, []string{})

	c.current = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Namespace:   "loki",
		Subsystem:   "cache",
		Name:        "entries",
		Help:        "Current number of items in the cache.",
		ConstLabels: prometheus.Labels{"cache": name},
	}, []string{})

	// Initialize LRU cache with a high size limit since we will manage evictions ourselves
	// based on stored size using `RemoveOldest` method.
	l, err := lru.NewLRU(maxInt, c.onEvict)
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
	c.evicted.WithLabelValues().Inc()
	c.current.WithLabelValues().Dec()
	c.bytesInUse.Sub(float64(c.entryMemoryUsage(key.(string), val.([]byte))))
}

func (c *LRUCache) get(key string) ([]byte, bool) {
	c.requests.WithLabelValues().Inc()

	c.mtx.Lock()
	defer c.mtx.Unlock()

	v, ok := c.lru.Get(key)
	if !ok {
		c.totalMisses.Inc()
		return nil, false
	}
	c.hits.WithLabelValues().Inc()
	return v.([]byte), true
}

func (c *LRUCache) set(key string, val []byte) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if _, ok := c.lru.Get(key); ok {
		return
	}

	// The caller may be passing in a sub-slice of a huge array. Copy the data
	// to ensure we don't waste huge amounts of space for something small.
	v := make([]byte, len(val))
	copy(v, val)
	c.lru.Add(key, v)

	c.bytesInUse.Add(float64(c.entryMemoryUsage(key, val)))
	c.added.WithLabelValues().Inc()
	c.current.WithLabelValues().Inc()
}

func (c *LRUCache) entryMemoryUsage(key string, val []byte) int {
	return int(unsafe.Sizeof(val)) + len(key)
}

func (c *LRUCache) reset() {
	c.lru.Purge()
	c.current.Reset()
	c.bytesInUse.Set(0)
}
