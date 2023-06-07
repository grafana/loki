// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/thanos-io/thanos/blob/main/pkg/store/cache/inmemory.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Thanos Authors.
package indexcache

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/dennwc/varint"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/grafana/loki/pkg/util/encoding"
	lru "github.com/hashicorp/golang-lru/simplelru"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

type codec string

const (
	codecHeaderSnappy             codec = "dvs" // As in "diff+varint+snappy".
	codecHeaderSnappyWithMatchers codec = "dm"  // As in "dvs+matchers"
)

var DefaultInMemoryIndexCacheConfig = InMemoryIndexCacheConfig{
	MaxSize:     250 * 1024 * 1024,
	MaxItemSize: 125 * 1024 * 1024,
}

const maxInt = int(^uint(0) >> 1)

const (
	stringHeaderSize = 8
	sliceHeaderSize  = 16
)

var ulidSize = uint64(len(ulid.ULID{}))

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
	MaxSize flagext.Bytes `yaml:"max_size"`
	// MaxItemSize represents maximum size of single item.
	MaxItemSize flagext.Bytes `yaml:"max_item_size"`
}

// NewInMemoryIndexCache creates a new thread-safe LRU cache for index entries and ensures the total cache
// size approximately does not exceed maxBytes.
func NewInMemoryIndexCache(logger log.Logger, reg prometheus.Registerer, cfg InMemoryIndexCacheConfig) (*InMemoryIndexCache, error) {
	return NewInMemoryIndexCacheWithConfig(logger, reg, cfg)
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
		Namespace: "loki",
		Name:      "index_gateway_index_cache_items_evicted_total",
		Help:      "Total number of items that were evicted from the index cache.",
	}, []string{"item_type"})
	initLabelValuesForAllCacheTypes(c.evicted.MetricVec)

	c.added = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "index_gateway_index_cache_items_added_total",
		Help:      "Total number of items that were added to the index cache.",
	}, []string{"item_type"})
	initLabelValuesForAllCacheTypes(c.added.MetricVec)

	c.requests = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "index_gateway_index_cache_requests_total",
		Help:      "Total number of requests to the cache.",
	}, []string{"item_type"})
	initLabelValuesForAllCacheTypes(c.requests.MetricVec)

	c.overflow = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "index_gateway_index_cache_items_overflowed_total",
		Help:      "Total number of items that could not be added to the cache due to being too big.",
	}, []string{"item_type"})
	initLabelValuesForAllCacheTypes(c.overflow.MetricVec)

	c.hits = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "index_gateway_index_cache_hits_total",
		Help:      "Total number of requests to the cache that were a hit.",
	}, []string{"item_type"})
	initLabelValuesForAllCacheTypes(c.hits.MetricVec)

	c.current = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "index_gateway_index_cache_items",
		Help:      "Current number of items in the index cache.",
	}, []string{"item_type"})
	initLabelValuesForAllCacheTypes(c.current.MetricVec)

	c.currentSize = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "index_gateway_index_cache_items_size_bytes",
		Help:      "Current byte size of items in the index cache.",
	}, []string{"item_type"})
	initLabelValuesForAllCacheTypes(c.currentSize.MetricVec)

	c.totalCurrentSize = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "index_gateway_index_cache_total_size_bytes",
		Help:      "Current byte size of items (both value and key) in the index cache.",
	}, []string{"item_type"})
	initLabelValuesForAllCacheTypes(c.totalCurrentSize.MetricVec)

	_ = promauto.With(reg).NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "index_gateway_index_cache_max_size_bytes",
		Help:      "Maximum number of bytes to be held in the index cache.",
	}, func() float64 {
		return float64(c.maxSizeBytes)
	})
	_ = promauto.With(reg).NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "index_gateway_index_cache_max_item_size_bytes",
		Help:      "Maximum number of bytes for single entry to be held in the index cache.",
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
	k := key.(cacheKey)
	typ := k.typ()
	entrySize := sliceSize(val.([]byte))

	c.evicted.WithLabelValues(typ).Inc()
	c.current.WithLabelValues(typ).Dec()
	c.currentSize.WithLabelValues(typ).Sub(float64(entrySize))
	c.totalCurrentSize.WithLabelValues(typ).Sub(float64(entrySize + k.size()))

	c.curSize -= entrySize
}

func (c *InMemoryIndexCache) get(key cacheKey) ([]byte, bool) {
	typ := key.typ()
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

func (c *InMemoryIndexCache) set(key cacheKey, val []byte) {
	typ := key.typ()
	size := sliceSize(val)

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

func (c *InMemoryIndexCache) StorePostings(postings index.Postings, matchers []*labels.Matcher) {
	dataToCache, err := diffVarintEncodeNoHeader(postings, 0)
	if err != nil {
		level.Warn(c.logger).Log("msg", "couldn't encode postings", "err", err, "matchers", CanonicalLabelMatchersKey(matchers))
	}

	c.set(cacheKeyPostings{matchers: matchers}, dataToCache)
}

// diffVarintEncodeNoHeader encodes postings into diff+varint representation.
// It doesn't add any header to the output bytes.
// Length argument is expected number of postings, used for preallocating buffer.
func diffVarintEncodeNoHeader(p index.Postings, length int) ([]byte, error) {
	buf := encoding.Encbuf{}

	// This encoding uses around ~1 bytes per posting, but let's use
	// conservative 1.25 bytes per posting to avoid extra allocations.
	if length > 0 {
		buf.B = make([]byte, 0, 5*length/4)
	}

	prev := storage.SeriesRef(0)
	for p.Next() {
		v := p.At()

		// TODO(dylanguedes): can we ignore this?
		// if v < prev {
		// 	return nil, errors.Errorf("postings entries must be in increasing order, current: %d, previous: %d", v, prev)
		// }

		// This is the 'diff' part -- compute difference from previous value.
		buf.PutUvarint64(uint64(v - prev))
		prev = v
	}
	if p.Err() != nil {
		return nil, p.Err()
	}

	return buf.B, nil
}

func encodedMatchersLen(matchers []*labels.Matcher) int {
	matchersLen := varint.UvarintSize(uint64(len(matchers)))
	for _, m := range matchers {
		matchersLen += varint.UvarintSize(uint64(len(m.Name)))
		matchersLen += len(m.Name)
		matchersLen++ // 1 byte for the type
		matchersLen += varint.UvarintSize(uint64(len(m.Value)))
		matchersLen += len(m.Value)
	}
	return matchersLen
}

// encodeMatchers needs to be called with the precomputed length of the encoded matchers from encodedMatchersLen
func encodeMatchers(expectedLen int, matchers []*labels.Matcher, dest []byte) (written int, _ error) {
	if len(dest) < expectedLen {
		return 0, fmt.Errorf("too small buffer to encode matchers: need at least %d, got %d", expectedLen, dest)
	}
	written += binary.PutUvarint(dest, uint64(len(matchers)))
	for _, m := range matchers {
		written += binary.PutUvarint(dest[written:], uint64(len(m.Name)))
		written += copy(dest[written:], m.Name)

		dest[written] = byte(m.Type)
		written++

		written += binary.PutUvarint(dest[written:], uint64(len(m.Value)))
		written += copy(dest[written:], m.Value)
	}
	return written, nil
}

// FetchSeriesForPostings fetches a series set for the provided postings.
func (c *InMemoryIndexCache) FetchSeriesForPostings(_ context.Context, matchers []*labels.Matcher) ([]byte, bool) {
	return c.get(cacheKeyPostings{matchers: matchers})
}

// cacheKey is used by in-memory representation to store cached data.
// The implementations of cacheKey should be hashable, as they will be used as keys for *lru.LRU cache
type cacheKey interface {
	// typ is used as label for metrics.
	typ() string
	// size is used to keep track of the cache size, it represents the footprint of the cache key in memory.
	size() uint64
}

// cacheKeyPostings implements cacheKey and is used to reference a postings cache entry in the inmemory cache.
type cacheKeyPostings struct {
	matchers []*labels.Matcher
}

func (c cacheKeyPostings) typ() string { return cacheTypePostings }

func (c cacheKeyPostings) size() uint64 {
	return stringSize(string(CanonicalLabelMatchersKey(c.matchers)))
}

func stringSize(s string) uint64 {
	return stringHeaderSize + uint64(len(s))
}

func sliceSize(b []byte) uint64 {
	return sliceHeaderSize + uint64(len(b))
}
