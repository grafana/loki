package bloomshipper

import (
	"container/list"
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	defaultPurgeInterval = 1 * time.Minute

	// eviction reasons
	reasonExpired = "expired"
	reasonFull    = "full"

	// errors when putting entries
	errAlreadyExists = "entry already exists: %s"
	errTooBig        = "entry exceeds hard limit: %s"
)

type Cache interface {
	Put(ctx context.Context, key string, value BlockDirectory) error
	PutInc(ctx context.Context, key string, value BlockDirectory) error
	PutMany(ctx context.Context, keys []string, values []BlockDirectory) error
	Get(ctx context.Context, key string) (BlockDirectory, bool)
	Release(ctx context.Context, key string) error
	Stop()
}

type blocksCacheMetrics struct {
	entriesAdded   prometheus.Counter
	entriesEvicted *prometheus.CounterVec
	entriesFetched *prometheus.CounterVec
	entriesCurrent prometheus.Gauge
	usageBytes     prometheus.Gauge

	// collecting hits/misses for every Get() is a performance killer
	// instead, increment a counter and collect them in an interval
	hits   *atomic.Int64
	misses *atomic.Int64
}

func newBlocksCacheMetrics(reg prometheus.Registerer, namespace, subsystem string) *blocksCacheMetrics {
	return &blocksCacheMetrics{
		entriesAdded: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "added_total",
			Help:      "The total number of entries added to the cache",
		}),
		entriesEvicted: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "evicted_total",
			Help:      "The total number of entries evicted from the cache",
		}, []string{"reason"}),
		entriesFetched: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "fetched_total",
			Help:      "Total number of entries fetched from cache, grouped by status",
		}, []string{"status"}),
		entriesCurrent: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "entries",
			Help:      "Current number of entries in the cache",
		}),
		usageBytes: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "usage_bytes",
			Help:      "The current size of entries managed by the cache in bytes",
		}),

		hits:   atomic.NewInt64(0),
		misses: atomic.NewInt64(0),
	}
}

func (m *blocksCacheMetrics) Collect() {
	m.entriesFetched.WithLabelValues("hit").Add(float64(m.hits.Swap(0)))
	m.entriesFetched.WithLabelValues("miss").Add(float64(m.misses.Swap(0)))
}

// compiler check to enforce implementation of the Cache interface
var _ Cache = &BlocksCache{}

// BlocksCache is an in-memory cache that manages block directories on the filesystem.
type BlocksCache struct {
	cfg     config.BlocksCacheConfig
	metrics *blocksCacheMetrics
	logger  log.Logger

	lock    sync.RWMutex // lock for cache entries
	entries map[string]*list.Element
	lru     *list.List

	done            chan struct{}
	triggerEviction chan struct{}

	currSizeBytes int64
}

type Entry struct {
	Key   string
	Value BlockDirectory

	created time.Time

	refCount *atomic.Int32
}

// NewFsBlocksCache returns a new file-system mapping cache for bloom blocks,
// where entries map block directories on disk.
func NewFsBlocksCache(cfg config.BlocksCacheConfig, reg prometheus.Registerer, logger log.Logger) *BlocksCache {
	cache := &BlocksCache{
		cfg:     cfg,
		logger:  logger,
		metrics: newBlocksCacheMetrics(reg, constants.Loki, "bloom_blocks_cache"),
		entries: make(map[string]*list.Element),
		lru:     list.New(),

		done:            make(chan struct{}),
		triggerEviction: make(chan struct{}, 1),
	}

	// Set a default interval for the ticker
	// This can be overwritten to a smaller value in tests
	if cfg.PurgeInterval == 0 {
		cfg.PurgeInterval = defaultPurgeInterval
	}

	go cache.runTTLEvictJob(cfg.PurgeInterval, cfg.TTL)
	go cache.runLRUevictJob()
	go cache.runMetricsCollectJob(5 * time.Second)

	return cache
}

// Put implements Cache.
// It stores a value with given key.
func (c *BlocksCache) Put(ctx context.Context, key string, value BlockDirectory) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	_, err := c.put(key, value)
	return err
}

// PutInc implements Cache.
// It stores a value with given key and increments the ref counter on that item.
func (c *BlocksCache) PutInc(ctx context.Context, key string, value BlockDirectory) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	entry, err := c.put(key, value)
	if err != nil {
		return err
	}

	entry.refCount.Inc()
	return nil
}

// PutMany implements Cache.
func (c *BlocksCache) PutMany(ctx context.Context, keys []string, values []BlockDirectory) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	var err util.MultiError
	for i := range keys {
		if _, e := c.put(keys[i], values[i]); e != nil {
			err.Add(e)
		}
	}
	return err.Err()
}

func (c *BlocksCache) put(key string, value BlockDirectory) (*Entry, error) {
	// Blocks cache does not allow updating, so it rejects values with the same key
	_, exists := c.entries[key]
	if exists {
		return nil, fmt.Errorf(errAlreadyExists, key)
	}

	entry := &Entry{
		Key:      key,
		Value:    value,
		created:  time.Now(),
		refCount: atomic.NewInt32(0),
	}
	size := entry.Value.Size()

	// Reject items that are larger than the hard limit
	if size > int64(c.cfg.HardLimit) {
		// It's safe to clean up the disk, since it does not have any references
		// yet. Ideally, we avoid downloading blocks that do not fit into the cache
		// upfront.
		_ = c.remove(entry)
		return nil, fmt.Errorf(errTooBig, key)
	}

	// Allow adding the new item even if the cache exceeds its soft limit.
	// However, broadcast the condition that the cache should be cleaned up.
	if c.currSizeBytes+size > int64(c.cfg.SoftLimit) {
		level.Debug(c.logger).Log(
			"msg", "adding item exceeds soft limit",
			"action", "trigger soft eviction",
			"curr_size_bytes", c.currSizeBytes,
			"entry_size_bytes", size,
			"soft_limit_bytes", c.cfg.SoftLimit,
			"hard_limit_bytes", c.cfg.HardLimit,
		)

		select {
		case c.triggerEviction <- struct{}{}:
			// nothing
		default:
			level.Debug(c.logger).Log("msg", "eviction already in progress")
		}
	}

	// Adding an item blocks if the cache would exceed its hard limit.
	if c.currSizeBytes+size > int64(c.cfg.HardLimit) {
		level.Debug(c.logger).Log(
			"msg", "adding item exceeds hard limit",
			"action", "evict items until space is freed",
			"curr_size_bytes", c.currSizeBytes,
			"entry_size_bytes", size,
			"soft_limit_bytes", c.cfg.SoftLimit,
			"hard_limit_bytes", c.cfg.HardLimit,
		)
		// TODO(chaudum): implement case
		return nil, errors.New("todo: implement waiting for evictions to free up space")
	}

	// Cache has space to add the item.
	c.entries[key] = c.lru.PushFront(entry)
	c.currSizeBytes += size

	c.metrics.entriesAdded.Inc()
	c.metrics.entriesCurrent.Inc()
	c.metrics.usageBytes.Set(float64(c.currSizeBytes))
	return entry, nil
}

func (c *BlocksCache) evict(key string, element *list.Element, reason string) {
	entry := element.Value.(*Entry)
	if key != entry.Key {
		level.Error(c.logger).Log("msg", "failed to remove entry: entry key and map key do not match", "map_key", key, "entry_key", entry.Key)
		return
	}
	err := c.remove(entry)
	if err != nil {
		level.Error(c.logger).Log("msg", "failed to remove entry from disk", "err", err)
		return
	}
	c.lru.Remove(element)
	delete(c.entries, key)
	c.currSizeBytes -= entry.Value.Size()
	c.metrics.entriesCurrent.Dec()
	c.metrics.entriesEvicted.WithLabelValues(reason).Inc()
}

// Get implements Cache.
// Get returns the stored value against the given key.
func (c *BlocksCache) Get(ctx context.Context, key string) (BlockDirectory, bool) {
	if ctx.Err() != nil {
		return BlockDirectory{}, false
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	entry := c.get(key)
	if entry == nil {
		return BlockDirectory{}, false
	}
	return entry.Value, true
}

func (c *BlocksCache) get(key string) *Entry {
	element, exists := c.entries[key]
	if !exists {
		c.metrics.misses.Inc()
		return nil
	}

	entry := element.Value.(*Entry)
	entry.refCount.Inc()

	c.lru.MoveToFront(element)

	c.metrics.hits.Inc()
	return entry
}

// Release decrements the ref counter on the cached item with given key.
func (c *BlocksCache) Release(ctx context.Context, key string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// We can use a read lock because we only update a field on an existing entry
	// and we do not modify the map of entries or the order in the LRU list.
	c.lock.RLock()
	defer c.lock.RUnlock()

	element, exists := c.entries[key]
	if !exists {
		return nil
	}

	entry := element.Value.(*Entry)
	entry.refCount.Dec()
	return nil
}

// Stop implements Cache.
func (c *BlocksCache) Stop() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.entries = make(map[string]*list.Element)
	c.lru.Init()

	c.metrics.entriesCurrent.Set(float64(0))
	c.metrics.usageBytes.Set(float64(0))

	close(c.done)
}

func (c *BlocksCache) remove(entry *Entry) error {
	level.Info(c.logger).Log("msg", "remove entry from disk", "path", entry.Value.Path)
	err := os.RemoveAll(entry.Value.Path)
	if err != nil {
		return fmt.Errorf("error removing bloom block directory from disk: %w", err)
	}
	return nil
}

func (c *BlocksCache) runMetricsCollectJob(interval time.Duration) {
	level.Info(c.logger).Log("msg", "run metrics collect job")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.metrics.Collect()
		}
	}
}

func (c *BlocksCache) runLRUevictJob() {
	level.Info(c.logger).Log("msg", "run lru evict job")
	for {
		select {
		case <-c.done:
			return
		case <-c.triggerEviction:
			c.evictLeastRecentlyUsedItems()
		}
	}
}

func (c *BlocksCache) evictLeastRecentlyUsedItems() {
	c.lock.Lock()
	defer c.lock.Unlock()

	level.Debug(c.logger).Log(
		"msg", "evict least recently used entries",
		"curr_size_bytes", c.currSizeBytes,
		"soft_limit_bytes", c.cfg.SoftLimit,
		"hard_limit_bytes", c.cfg.HardLimit,
	)
	elem := c.lru.Back()
	for c.currSizeBytes >= int64(c.cfg.SoftLimit) && elem != nil {
		nextElem := elem.Prev()
		entry := elem.Value.(*Entry)
		if entry.refCount.Load() == 0 {
			level.Debug(c.logger).Log(
				"msg", "evict least recently used entry",
				"entry", entry.Key,
			)
			c.evict(entry.Key, elem, reasonFull)
		}
		elem = nextElem
	}
}

func (c *BlocksCache) runTTLEvictJob(interval, ttl time.Duration) {
	if interval == 0 || ttl == 0 {
		return
	}

	level.Info(c.logger).Log("msg", "run ttl evict job")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.evictExpiredItems(ttl)
		}
	}
}

// evictExpiredItems prunes items in the cache that exceeded their ttl after last access
func (c *BlocksCache) evictExpiredItems(ttl time.Duration) {
	c.lock.Lock()
	defer c.lock.Unlock()

	level.Debug(c.logger).Log(
		"msg", "evict expired entries",
		"curr_size_bytes", c.currSizeBytes,
		"soft_limit_bytes", c.cfg.SoftLimit,
		"hard_limit_bytes", c.cfg.HardLimit,
	)
	for k, v := range c.entries {
		entry := v.Value.(*Entry)
		if time.Since(entry.created) > ttl && entry.refCount.Load() == 0 {
			level.Debug(c.logger).Log(
				"msg", "evict expired entry",
				"entry", entry.Key,
				"age", time.Since(entry.created),
				"ttl", ttl,
			)
			c.evict(k, v, reasonExpired)
		}
	}
}

func (c *BlocksCache) len() (int, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.lru.Len(), c.lru.Len() == len(c.entries)
}
