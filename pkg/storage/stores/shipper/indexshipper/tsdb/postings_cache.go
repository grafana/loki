package tsdb

import (
	"container/list"
	"context"
	"encoding/binary"
	"reflect"
	"slices"
	"strings"
	"time"

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
	cache *cache.EmbeddedCache[string, []storage.SeriesRef]

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
	// Metric names, labels and buckets match cache.Instrument so this cache
	// appears on the same dashboards as every other cache.
	valueSize := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   constants.Loki,
		Name:        "cache_value_size_bytes",
		Help:        "Size of values in the cache.",
		Buckets:     prometheus.ExponentialBuckets(1024, 4, 7),
		ConstLabels: prometheus.Labels{"name": name},
	}, []string{"method"})
	duration := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   constants.Loki,
		Name:        "cache_request_duration_seconds",
		Help:        "Total time spent in seconds doing cache requests.",
		Buckets:     prometheus.ExponentialBuckets(0.000016, 4, 8),
		ConstLabels: prometheus.Labels{"name": name},
	}, []string{"method", "status_code"})
	return &postingsCache{
		cache: embedded,
		fetchedKeys: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   constants.Loki,
			Name:        "cache_fetched_keys",
			Help:        "Total count of keys requested from cache.",
			ConstLabels: prometheus.Labels{"name": name},
		}),
		hits: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   constants.Loki,
			Name:        "cache_hits",
			Help:        "Total count of keys found in cache.",
			ConstLabels: prometheus.Labels{"name": name},
		}),
		fetchedSize:   valueSize.WithLabelValues("fetch"),
		storedSize:    valueSize.WithLabelValues("store"),
		fetchDuration: duration.WithLabelValues(name+".fetch", "200"),
		storeDuration: duration.WithLabelValues(name+".store", "200"),
	}
}

var (
	// seriesRefSize is computed once so cache size observations only multiply lengths.
	seriesRefSize = int(reflect.TypeFor[storage.SeriesRef]().Size())
	// postingsEntryOverhead mirrors the embedded cache's own accounting for byte
	// values: the entry struct, its list element, and the map slot pointing to it.
	postingsEntryOverhead = reflect.TypeFor[cache.Entry[string, []storage.SeriesRef]]().Size() +
		reflect.TypeFor[list.Element]().Size() +
		reflect.TypeFor[*list.Element]().Size()
)

// postingsSize is the number of bytes occupied by the references in use.
func postingsSize(refs []storage.SeriesRef) int {
	return len(refs) * seriesRefSize
}

// postingsEntrySize accounts for the retained backing array, not just its used
// length. This estimates per-entry memory, not Go allocator overhead.
func postingsEntrySize(entry *cache.Entry[string, []storage.SeriesRef]) uint64 {
	return uint64(postingsEntryOverhead) + uint64(len(entry.Key)) + uint64(cap(entry.Value)*seriesRefSize)
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

// cachedPostings returns the postings for key, computing and caching them on a
// miss. The returned iterator and the cache share one read-only array.
func (c *postingsCache) cachedPostings(ctx context.Context, key string, compute func() (index.Postings, error)) (index.Postings, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p := c.fetchPostings(ctx, key); p != nil {
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
	refs = compactPostings(refs)
	c.store(ctx, key, refs)
	return index.NewListPostings(refs), nil
}

// compactPostings returns refs in a backing array with no spare capacity.
// ExpandPostings grows its result by appending, which can leave the array up to
// twice as large as the data, and the cache charges the whole array against its
// memory budget.
func compactPostings(refs []storage.SeriesRef) []storage.SeriesRef {
	if len(refs) == cap(refs) {
		return refs
	}
	compact := make([]storage.SeriesRef, len(refs))
	copy(compact, refs)
	return compact
}

// fetchPostings returns an iterator over the cached references for key, or nil
// on a miss. An empty value is a cached empty result, not a miss.
func (c *postingsCache) fetchPostings(ctx context.Context, key string) index.Postings {
	start := time.Now()
	refs, hit := c.cache.Get(ctx, key)
	elapsed := time.Since(start)
	c.fetchDuration.Observe(elapsed.Seconds())
	c.fetchedKeys.Inc()
	st := stats.FromContext(ctx)
	st.AddCacheRequest(stats.IndexCache, 1)
	st.AddCacheEntriesRequested(stats.IndexCache, 1)
	st.AddCacheDownloadTime(stats.IndexCache, elapsed)
	if !hit {
		return nil
	}
	size := postingsSize(refs)
	c.hits.Inc()
	c.fetchedSize.Observe(float64(size))
	st.AddCacheEntriesFound(stats.IndexCache, 1)
	st.AddCacheBytesRetrieved(stats.IndexCache, size)
	return index.NewListPostings(refs)
}

// store caches refs for key. Results larger than the memory budget are rejected
// by the embedded cache, which cannot otherwise fail to store.
func (c *postingsCache) store(ctx context.Context, key string, refs []storage.SeriesRef) {
	st := stats.FromContext(ctx)
	st.AddCacheRequest(stats.IndexCache, 1)
	st.AddCacheEntriesStored(stats.IndexCache, 1)
	c.storedSize.Observe(float64(postingsSize(refs)))
	start := time.Now()
	_ = c.cache.Store(ctx, []string{key}, [][]storage.SeriesRef{refs})
	c.storeDuration.Observe(time.Since(start).Seconds())
}

// Stop releases the cached references.
func (c *postingsCache) Stop() {
	c.cache.Stop()
}
