package tsdb

import (
	"context"
	"encoding/binary"
	"slices"
	"strings"
	"unsafe"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// postingsCache stores immutable, decoded references in process. Iterators share
// their backing arrays but maintain independent positions.
type postingsCache struct {
	cache cache.TypedCache[string, []storage.SeriesRef]
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
	return &postingsCache{
		cache: cache.CollectTypedStats(cache.InstrumentTyped(name, embedded, reg, postingsSize), postingsSize),
	}
}

// postingsSize is the number of bytes occupied by the references in use.
func postingsSize(refs []storage.SeriesRef) int {
	return len(refs) * int(unsafe.Sizeof(storage.SeriesRef(0)))
}

// postingsEntrySize accounts for the retained backing array, not just its used
// length. This estimates per-entry memory, not Go allocator overhead.
func postingsEntrySize(entry *cache.Entry[string, []storage.SeriesRef]) uint64 {
	return cache.EntryOverhead(entry) + uint64(len(entry.Key)) + uint64(cap(entry.Value))*uint64(unsafe.Sizeof(storage.SeriesRef(0)))
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
	// Results larger than the memory budget are rejected by the embedded cache,
	// which cannot otherwise fail to store.
	_ = c.cache.Store(ctx, []string{key}, [][]storage.SeriesRef{refs})
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
	_, values, _, err := c.cache.Fetch(ctx, []string{key})
	if err != nil || len(values) == 0 {
		return nil
	}
	return index.NewListPostings(values[0])
}

// Stop releases the cached references.
func (c *postingsCache) Stop() {
	c.cache.Stop()
}
