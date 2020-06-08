// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package storecache

import (
	"context"
	"reflect"
	"sync"
	"unsafe"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	lru "github.com/hashicorp/golang-lru/simplelru"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/thanos-io/thanos/pkg/model"
	"gopkg.in/yaml.v2"
)

var (
	DefaultInMemoryIndexCacheConfig = InMemoryIndexCacheConfig{
		MaxSize:     250 * 1024 * 1024,
		MaxItemSize: 125 * 1024 * 1024,
	}
)

const maxInt = int(^uint(0) >> 1)

type InMemoryIndexCache struct {
	mtx sync.Mutex

	logger           log.Logger
	lru              *lru.LRU
	maxSizeBytes     uint64
	maxItemSizeBytes uint64

	curSize uint64

	evicted          *prometheus.CounterVec
	requests         *prometheus.CounterVec
	hits             *prometheus.CounterVec
	added            *prometheus.CounterVec
	current          *prometheus.GaugeVec
	currentSize      *prometheus.GaugeVec
	totalCurrentSize *prometheus.GaugeVec
	overflow         *prometheus.CounterVec
}

// InMemoryIndexCacheConfig holds the in-memory index cache config.
type InMemoryIndexCacheConfig struct {
	// MaxSize represents overall maximum number of bytes cache can contain.
	MaxSize model.Bytes `yaml:"max_size"`
	// MaxItemSize represents maximum size of single item.
	MaxItemSize model.Bytes `yaml:"max_item_size"`
}

// parseInMemoryIndexCacheConfig unmarshals a buffer into a InMemoryIndexCacheConfig with default values.
func parseInMemoryIndexCacheConfig(conf []byte) (InMemoryIndexCacheConfig, error) {
	config := DefaultInMemoryIndexCacheConfig
	if err := yaml.Unmarshal(conf, &config); err != nil {
		return InMemoryIndexCacheConfig{}, err
	}

	return config, nil
}

// NewInMemoryIndexCache creates a new thread-safe LRU cache for index entries and ensures the total cache
// size approximately does not exceed maxBytes.
func NewInMemoryIndexCache(logger log.Logger, reg prometheus.Registerer, conf []byte) (*InMemoryIndexCache, error) {
	config, err := parseInMemoryIndexCacheConfig(conf)
	if err != nil {
		return nil, err
	}

	return NewInMemoryIndexCacheWithConfig(logger, reg, config)
}

// NewInMemoryIndexCacheWithConfig creates a new thread-safe LRU cache for index entries and ensures the total cache
// size approximately does not exceed maxBytes.
func NewInMemoryIndexCacheWithConfig(logger log.Logger, reg prometheus.Registerer, config InMemoryIndexCacheConfig) (*InMemoryIndexCache, error) {
	if config.MaxItemSize > config.MaxSize {
		return nil, errors.Errorf("max item size (%v) cannot be bigger than overall cache size (%v)", config.MaxItemSize, config.MaxSize)
	}

	c := &InMemoryIndexCache{
		logger:           logger,
		maxSizeBytes:     uint64(config.MaxSize),
		maxItemSizeBytes: uint64(config.MaxItemSize),
	}

	c.evicted = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thanos_store_index_cache_items_evicted_total",
		Help: "Total number of items that were evicted from the index cache.",
	}, []string{"item_type"})
	c.evicted.WithLabelValues(cacheTypePostings)
	c.evicted.WithLabelValues(cacheTypeSeries)

	c.added = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thanos_store_index_cache_items_added_total",
		Help: "Total number of items that were added to the index cache.",
	}, []string{"item_type"})
	c.added.WithLabelValues(cacheTypePostings)
	c.added.WithLabelValues(cacheTypeSeries)

	c.requests = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thanos_store_index_cache_requests_total",
		Help: "Total number of requests to the cache.",
	}, []string{"item_type"})
	c.requests.WithLabelValues(cacheTypePostings)
	c.requests.WithLabelValues(cacheTypeSeries)

	c.overflow = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thanos_store_index_cache_items_overflowed_total",
		Help: "Total number of items that could not be added to the cache due to being too big.",
	}, []string{"item_type"})
	c.overflow.WithLabelValues(cacheTypePostings)
	c.overflow.WithLabelValues(cacheTypeSeries)

	c.hits = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thanos_store_index_cache_hits_total",
		Help: "Total number of requests to the cache that were a hit.",
	}, []string{"item_type"})
	c.hits.WithLabelValues(cacheTypePostings)
	c.hits.WithLabelValues(cacheTypeSeries)

	c.current = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "thanos_store_index_cache_items",
		Help: "Current number of items in the index cache.",
	}, []string{"item_type"})
	c.current.WithLabelValues(cacheTypePostings)
	c.current.WithLabelValues(cacheTypeSeries)

	c.currentSize = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "thanos_store_index_cache_items_size_bytes",
		Help: "Current byte size of items in the index cache.",
	}, []string{"item_type"})
	c.currentSize.WithLabelValues(cacheTypePostings)
	c.currentSize.WithLabelValues(cacheTypeSeries)

	c.totalCurrentSize = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "thanos_store_index_cache_total_size_bytes",
		Help: "Current byte size of items (both value and key) in the index cache.",
	}, []string{"item_type"})
	c.totalCurrentSize.WithLabelValues(cacheTypePostings)
	c.totalCurrentSize.WithLabelValues(cacheTypeSeries)

	_ = promauto.With(reg).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "thanos_store_index_cache_max_size_bytes",
		Help: "Maximum number of bytes to be held in the index cache.",
	}, func() float64 {
		return float64(c.maxSizeBytes)
	})
	_ = promauto.With(reg).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "thanos_store_index_cache_max_item_size_bytes",
		Help: "Maximum number of bytes for single entry to be held in the index cache.",
	}, func() float64 {
		return float64(c.maxItemSizeBytes)
	})

	// Initialize LRU cache with a high size limit since we will manage evictions ourselves
	// based on stored size using `RemoveOldest` method.
	l, err := lru.NewLRU(maxInt, c.onEvict)
	if err != nil {
		return nil, err
	}
	c.lru = l

	level.Info(logger).Log(
		"msg", "created in-memory index cache",
		"maxItemSizeBytes", c.maxItemSizeBytes,
		"maxSizeBytes", c.maxSizeBytes,
		"maxItems", "maxInt",
	)
	return c, nil
}

func (c *InMemoryIndexCache) onEvict(key, val interface{}) {
	k := key.(cacheKey).keyType()
	entrySize := sliceHeaderSize + uint64(len(val.([]byte)))

	c.evicted.WithLabelValues(string(k)).Inc()
	c.current.WithLabelValues(string(k)).Dec()
	c.currentSize.WithLabelValues(string(k)).Sub(float64(entrySize))
	c.totalCurrentSize.WithLabelValues(string(k)).Sub(float64(entrySize + key.(cacheKey).size()))

	c.curSize -= entrySize
}

func (c *InMemoryIndexCache) get(typ string, key cacheKey) ([]byte, bool) {
	c.requests.WithLabelValues(typ).Inc()

	c.mtx.Lock()
	defer c.mtx.Unlock()

	v, ok := c.lru.Get(key)
	if !ok {
		return nil, false
	}
	c.hits.WithLabelValues(typ).Inc()
	return v.([]byte), true
}

func (c *InMemoryIndexCache) set(typ string, key cacheKey, val []byte) {
	var size = sliceHeaderSize + uint64(len(val))

	c.mtx.Lock()
	defer c.mtx.Unlock()

	if _, ok := c.lru.Get(key); ok {
		return
	}

	if !c.ensureFits(size, typ) {
		c.overflow.WithLabelValues(typ).Inc()
		return
	}

	// The caller may be passing in a sub-slice of a huge array. Copy the data
	// to ensure we don't waste huge amounts of space for something small.
	v := make([]byte, len(val))
	copy(v, val)
	c.lru.Add(key, v)

	c.added.WithLabelValues(typ).Inc()
	c.currentSize.WithLabelValues(typ).Add(float64(size))
	c.totalCurrentSize.WithLabelValues(typ).Add(float64(size + key.size()))
	c.current.WithLabelValues(typ).Inc()
	c.curSize += size
}

// ensureFits tries to make sure that the passed slice will fit into the LRU cache.
// Returns true if it will fit.
func (c *InMemoryIndexCache) ensureFits(size uint64, typ string) bool {
	if size > c.maxItemSizeBytes {
		level.Debug(c.logger).Log(
			"msg", "item bigger than maxItemSizeBytes. Ignoring..",
			"maxItemSizeBytes", c.maxItemSizeBytes,
			"maxSizeBytes", c.maxSizeBytes,
			"curSize", c.curSize,
			"itemSize", size,
			"cacheType", typ,
		)
		return false
	}

	for c.curSize+size > c.maxSizeBytes {
		if _, _, ok := c.lru.RemoveOldest(); !ok {
			level.Error(c.logger).Log(
				"msg", "LRU has nothing more to evict, but we still cannot allocate the item. Resetting cache.",
				"maxItemSizeBytes", c.maxItemSizeBytes,
				"maxSizeBytes", c.maxSizeBytes,
				"curSize", c.curSize,
				"itemSize", size,
				"cacheType", typ,
			)
			c.reset()
		}
	}
	return true
}

func (c *InMemoryIndexCache) reset() {
	c.lru.Purge()
	c.current.Reset()
	c.currentSize.Reset()
	c.totalCurrentSize.Reset()
	c.curSize = 0
}

func copyString(s string) string {
	var b []byte
	h := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	h.Data = (*reflect.StringHeader)(unsafe.Pointer(&s)).Data
	h.Len = len(s)
	h.Cap = len(s)
	return string(b)
}

// copyToKey is required as underlying strings might be mmaped.
func copyToKey(l labels.Label) cacheKeyPostings {
	return cacheKeyPostings(labels.Label{Value: copyString(l.Value), Name: copyString(l.Name)})
}

// StorePostings sets the postings identified by the ulid and label to the value v,
// if the postings already exists in the cache it is not mutated.
func (c *InMemoryIndexCache) StorePostings(_ context.Context, blockID ulid.ULID, l labels.Label, v []byte) {
	c.set(cacheTypePostings, cacheKey{block: blockID, key: copyToKey(l)}, v)
}

// FetchMultiPostings fetches multiple postings - each identified by a label -
// and returns a map containing cache hits, along with a list of missing keys.
func (c *InMemoryIndexCache) FetchMultiPostings(_ context.Context, blockID ulid.ULID, keys []labels.Label) (hits map[labels.Label][]byte, misses []labels.Label) {
	hits = map[labels.Label][]byte{}

	for _, key := range keys {
		if b, ok := c.get(cacheTypePostings, cacheKey{blockID, cacheKeyPostings(key)}); ok {
			hits[key] = b
			continue
		}

		misses = append(misses, key)
	}

	return hits, misses
}

// StoreSeries sets the series identified by the ulid and id to the value v,
// if the series already exists in the cache it is not mutated.
func (c *InMemoryIndexCache) StoreSeries(_ context.Context, blockID ulid.ULID, id uint64, v []byte) {
	c.set(cacheTypeSeries, cacheKey{blockID, cacheKeySeries(id)}, v)
}

// FetchMultiSeries fetches multiple series - each identified by ID - from the cache
// and returns a map containing cache hits, along with a list of missing IDs.
func (c *InMemoryIndexCache) FetchMultiSeries(_ context.Context, blockID ulid.ULID, ids []uint64) (hits map[uint64][]byte, misses []uint64) {
	hits = map[uint64][]byte{}

	for _, id := range ids {
		if b, ok := c.get(cacheTypeSeries, cacheKey{blockID, cacheKeySeries(id)}); ok {
			hits[id] = b
			continue
		}

		misses = append(misses, id)
	}

	return hits, misses
}
