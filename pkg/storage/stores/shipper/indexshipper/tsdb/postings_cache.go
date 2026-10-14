package tsdb

import (
	"container/list"
	"context"
	"encoding/binary"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// postingsCache stores immutable, decoded references in process. Iterators share
// their backing arrays but maintain independent positions.
type postingsCache struct {
	cache         *cache.EmbeddedCache[string, []storage.SeriesRef]
	mu            sync.Mutex // Held by at most one async insertion, or by Stop.
	stopped       bool
	maxEntryBytes uint64

	fetchedKeys, hits prometheus.Counter
	// Fetch samples are observed on each hit; store samples on each insertion.
	fetchedSize, storedSize      prometheus.Observer
	fetchDuration, storeDuration prometheus.Observer
}

func newPostingsCache(cfg cache.EmbeddedCacheConfig, name string, reg prometheus.Registerer, logger log.Logger) *postingsCache {
	if !cfg.Enabled {
		return nil
	}
	embedded := cache.NewTypedEmbeddedCache[string, []storage.SeriesRef](
		name, cfg, reg, logger, stats.IndexCache, postingsEntrySize, nil,
	)
	if embedded == nil {
		return nil
	}
	valueSize := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki, Name: "cache_value_size_bytes",
		Help:                            "Size of values in the cache.",
		NativeHistogramBucketFactor:     1.1,
		NativeHistogramMaxBucketNumber:  100,
		NativeHistogramMinResetDuration: time.Hour,
		ConstLabels:                     prometheus.Labels{"name": name},
	}, []string{"method"})
	duration := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki, Name: "cache_request_duration_seconds",
		Help:                            "Total time spent in seconds doing cache requests.",
		NativeHistogramBucketFactor:     1.1,
		NativeHistogramMaxBucketNumber:  100,
		NativeHistogramMinResetDuration: time.Hour,
		ConstLabels:                     prometheus.Labels{"name": name},
	}, []string{"method", "status_code"})
	return &postingsCache{
		cache:         embedded,
		maxEntryBytes: uint64(cfg.MaxSizeMB) * 1e6,
		fetchedKeys: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki, Name: "cache_fetched_keys",
			Help: "Total count of keys requested from cache.", ConstLabels: prometheus.Labels{"name": name},
		}),
		hits: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki, Name: "cache_hits",
			Help: "Total count of keys found in cache.", ConstLabels: prometheus.Labels{"name": name},
		}),
		fetchedSize:   valueSize.WithLabelValues("fetch"),
		storedSize:    valueSize.WithLabelValues("store"),
		fetchDuration: duration.WithLabelValues(name+".fetch", "200"),
		storeDuration: duration.WithLabelValues(name+".store", "200"),
	}
}

// postingsEntrySize accounts for the retained backing array, not just its used
// length, plus the entry, key bytes, list element and map slot. This estimates
// per-entry memory, not Go allocator overhead.
func postingsEntrySize(entry *cache.Entry[string, []storage.SeriesRef]) uint64 {
	return uint64(unsafe.Sizeof(*entry)) +
		uint64(len(entry.Key)) +
		uint64(cap(entry.Value))*uint64(unsafe.Sizeof(storage.SeriesRef(0))) +
		uint64(unsafe.Sizeof(list.Element{})) +
		uint64(unsafe.Sizeof(entry.Key)) + uint64(unsafe.Sizeof((*list.Element)(nil)))
}

func objectIdentity(prefix, table, tenant, filename string) string {
	return encodeKeyParts(prefix, table, tenant, filename)
}

func encodeKeyParts(parts ...string) string {
	size := len(parts) * binary.MaxVarintLen64
	for _, part := range parts {
		size += len(part)
	}

	buf := make([]byte, 0, size)
	for _, part := range parts {
		buf = binary.AppendUvarint(buf, uint64(len(part)))
		buf = append(buf, part...)
	}
	return string(buf)
}

func postingsKey(identity string, matchers []*labels.Matcher) string {
	canonical := make([]labels.Matcher, len(matchers))
	for i, matcher := range matchers {
		canonical[i] = labels.Matcher{Type: matcher.Type, Name: matcher.Name, Value: matcher.Value}
	}
	slices.SortFunc(canonical, func(a, b labels.Matcher) int {
		if result := strings.Compare(a.Name, b.Name); result != 0 {
			return result
		}
		if result := int(a.Type - b.Type); result != 0 {
			return result
		}
		return strings.Compare(a.Value, b.Value)
	})

	const matcherTypeLen = 2
	size := 0
	for _, matcher := range canonical {
		size += len(matcher.Name) + matcherTypeLen + binary.Size(uint64(len(matcher.Value))) + len(matcher.Value)
	}
	canonicalMatchers := make([]byte, 0, size)
	for _, matcher := range canonical {
		canonicalMatchers = append(canonicalMatchers, matcher.Name...)
		canonicalMatchers = append(canonicalMatchers, matcher.Type.String()...)
		// A fixed-width value length prevents ambiguity with matcher operators.
		// Appending a uint64 cannot fail.
		canonicalMatchers, _ = binary.Append(canonicalMatchers, binary.BigEndian, uint64(len(matcher.Value)))
		canonicalMatchers = append(canonicalMatchers, matcher.Value...)
	}

	// A distinct key namespace separates complete results from legacy sharded entries.
	return encodeKeyParts("unsharded-v1", identity, string(canonicalMatchers))
}

func (c *postingsCache) cachedPostings(ctx context.Context, key string, compute func() (index.Postings, error)) (index.Postings, error) {
	p := c.fetchPostings(ctx, key)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p != nil {
		return p, nil
	}
	postings, err := compute()
	if err != nil {
		return nil, err
	}
	refs, err := index.ExpandPostings(postings)
	if err != nil {
		return nil, err
	}
	slices.Sort(refs)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// ExpandPostings owns this array. The cache and iterators only read it.
	c.store(ctx, key, refs)
	return index.NewListPostings(refs), nil
}

func (c *postingsCache) fetchPostings(ctx context.Context, key string) index.Postings {
	if ctx.Err() != nil {
		return nil
	}
	refs, hit := c.fetch(ctx, key)
	if !hit {
		return nil
	}
	// A nil refs slice is a cached empty result, not a miss.
	return index.NewListPostings(refs)
}

// Stop waits for the active insertion, releases cached references, and prevents subsequent stores.
func (c *postingsCache) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return
	}
	c.stopped = true
	c.cache.Stop()
}

func (c *postingsCache) fetch(ctx context.Context, key string) ([]storage.SeriesRef, bool) {
	start := time.Now()
	refs, hit := c.cache.Get(ctx, key)
	elapsed := time.Since(start)
	c.fetchDuration.Observe(elapsed.Seconds())
	c.fetchedKeys.Inc()
	st := stats.FromContext(ctx)
	st.AddCacheRequest(stats.IndexCache, 1)
	st.AddCacheEntriesRequested(stats.IndexCache, 1)
	st.AddCacheDownloadTime(stats.IndexCache, elapsed)
	if hit {
		size := len(refs) * int(unsafe.Sizeof(storage.SeriesRef(0)))
		c.hits.Inc()
		c.fetchedSize.Observe(float64(size))
		st.AddCacheEntriesFound(stats.IndexCache, 1)
		st.AddCacheBytesRetrieved(stats.IndexCache, size)
	}
	return refs, hit
}

// store starts at most one insertion at a time, without queuing or waiting for
// either the lifecycle lock or the embedded cache's insertion lock.
func (c *postingsCache) store(ctx context.Context, key string, refs []storage.SeriesRef) {
	if ctx.Err() != nil {
		return
	}
	if c.maxEntryBytes > 0 && postingsEntrySize(&cache.Entry[string, []storage.SeriesRef]{Key: key, Value: refs}) > c.maxEntryBytes {
		return
	}
	if !c.mu.TryLock() {
		return // Cache population is best effort when another insertion is active.
	}
	if c.stopped {
		c.mu.Unlock()
		return
	}
	// Record request statistics before returning, rather than retaining the
	// request context in the worker after the query has finished.
	st := stats.FromContext(ctx)
	st.AddCacheRequest(stats.IndexCache, 1)
	st.AddCacheEntriesStored(stats.IndexCache, 1)
	go func() {
		// Go mutexes are not goroutine-owned: the worker releases the lock
		// acquired above. Stop waits for this insertion before clearing the cache.
		defer c.mu.Unlock()
		c.storedSize.Observe(float64(len(refs) * int(unsafe.Sizeof(storage.SeriesRef(0)))))
		start := time.Now()
		defer func() { c.storeDuration.Observe(time.Since(start).Seconds()) }()
		if err := c.cache.Store(context.Background(), []string{key}, [][]storage.SeriesRef{refs}); err != nil {
			return // Best effort; the embedded implementation currently cannot fail.
		}
	}()
}
