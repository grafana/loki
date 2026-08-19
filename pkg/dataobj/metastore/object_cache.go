package metastore

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// sectionsCacheKeyPrefix namespaces every key so these entries never collide with other subsystems'
// keys in a shared cache. The trailing slash keeps the namespace distinct from any key that merely
// starts with the same letters.
const sectionsCacheKeyPrefix = "metastore-sections/"

// sectionsCacheKeyVersion follows the prefix in every cache key. Bump it when the resolution logic or
// the cached-value encoding changes in a way that makes old entries wrong.
const sectionsCacheKeyVersion = "v1"

// SectionsCache caches ObjectMetastore.Sections() resolutions. An ObjectMetastore always holds one; when
// none is plugged via WithSectionsCache it uses a no-op cache that always misses.
type SectionsCache interface {
	// Get returns the cached section descriptors for (tenant, req, indexes), or false on any miss
	// (absent, cache error, undecodable entry, or key collision).
	Get(ctx context.Context, tenant string, req SectionsRequest, indexes []IndexEntry) ([]*DataobjSectionDescriptor, bool)

	// Put stores sections for (tenant, req, indexes) together with those inputs for collision detection.
	Put(ctx context.Context, tenant string, req SectionsRequest, indexes []IndexEntry, sections []*DataobjSectionDescriptor)
}

// noopSectionsCache is the SectionsCache used when none is plugged in. It always misses and drops writes,
// and holds no counters, so a metastore without a cache exposes no cache metrics.
type noopSectionsCache struct{}

func (noopSectionsCache) Get(context.Context, string, SectionsRequest, []IndexEntry) ([]*DataobjSectionDescriptor, bool) {
	return nil, false
}

func (noopSectionsCache) Put(context.Context, string, SectionsRequest, []IndexEntry, []*DataobjSectionDescriptor) {
}

// sectionsCache stores Sections() resolutions in a cache.Cache. It works with high-level types: callers
// pass the resolution inputs (tenant, request, index-object set) and the section descriptors, and the
// cache handles keying, serialization, and collision detection. It owns its hit/miss counters.
type sectionsCache struct {
	cache  cache.Cache
	logger log.Logger

	hits   prometheus.Counter
	misses prometheus.Counter
}

// NewSectionsCache builds a SectionsCache backed by c, registering its hit/miss counters on reg. c must
// be non-nil (a no-op cache is used defensively otherwise).
func NewSectionsCache(c cache.Cache, reg prometheus.Registerer, logger log.Logger) SectionsCache {
	if c == nil {
		c = cache.NewNoopCache()
	}
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &sectionsCache{
		cache:  c,
		logger: logger,
		hits: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_metastore_sections_cache_hits_total",
			Help: "Sections resolutions served from the cache (either layer).",
		}),
		misses: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_metastore_sections_cache_misses_total",
			Help: "Sections resolutions that had to run the metastore lookup.",
		}),
	}
}

// Get records the hit/miss counter on every call. A cache error, undecodable entry, or key collision
// degrades to a miss so a broken cache never fails the query or returns a wrong answer.
func (c *sectionsCache) Get(ctx context.Context, tenant string, req SectionsRequest, indexes []IndexEntry) (sections []*DataobjSectionDescriptor, hit bool) {
	defer func() {
		if hit {
			c.hits.Inc()
		} else {
			c.misses.Inc()
		}
	}()

	matchers := stableMatchers(req.Matchers)
	predicates := stableMatchers(req.Predicates)
	sortedIndexes := stableIndexEntries(indexes)
	key := hashSectionsCacheKey(tenant, req.Start.UnixNano(), req.End.UnixNano(), matchers, predicates, sortedIndexes)

	_, bufs, _, err := c.cache.Fetch(ctx, []string{key})
	if err != nil {
		level.Warn(c.logger).Log("msg", "sections cache fetch failed", "err", err)
		return nil, false
	}
	if len(bufs) != 1 {
		return nil, false
	}

	var entry CachedSections
	if err := entry.Unmarshal(bufs[0]); err != nil {
		level.Warn(c.logger).Log("msg", "discarding undecodable cached sections", "err", err)
		return nil, false
	}
	if entry.Matchers != matchers || entry.Predicates != predicates ||
		entry.StartNanos != req.Start.UnixNano() || entry.EndNanos != req.End.UnixNano() ||
		!cachedIndexesMatch(entry.Indexes, sortedIndexes) {
		// A mismatch here is almost never a real hash collision; it flags a key/encoding bug that would
		// otherwise show only as a low hit ratio. Log the inputs so it is diagnosable.
		level.Warn(c.logger).Log("msg", "sections cache key collision; treating as miss",
			"key", key, "tenant", tenant, "start", req.Start, "end", req.End)
		return nil, false
	}
	return sectionsFromCached(entry.Sections), true
}

// Put stores sections for (tenant, req, indexes) together with those inputs (for collision detection on
// read). Writes to the shared (memcached) layer are asynchronous, so Put does not block the caller on it.
func (c *sectionsCache) Put(ctx context.Context, tenant string, req SectionsRequest, indexes []IndexEntry, sections []*DataobjSectionDescriptor) {
	matchers := stableMatchers(req.Matchers)
	predicates := stableMatchers(req.Predicates)
	sortedIndexes := stableIndexEntries(indexes)
	key := hashSectionsCacheKey(tenant, req.Start.UnixNano(), req.End.UnixNano(), matchers, predicates, sortedIndexes)

	entry := CachedSections{
		Matchers:   matchers,
		Predicates: predicates,
		StartNanos: req.Start.UnixNano(),
		EndNanos:   req.End.UnixNano(),
		Indexes:    toCachedIndexEntries(sortedIndexes),
		Sections:   toCachedSectionDescriptors(sections),
	}
	b, err := entry.Marshal()
	if err != nil {
		level.Warn(c.logger).Log("msg", "marshalling sections for cache failed", "err", err)
		return
	}
	if err := c.cache.Store(ctx, []string{key}, [][]byte{b}); err != nil {
		level.Warn(c.logger).Log("msg", "sections cache store failed", "err", err)
	}
}

// sectionsCacheKey derives the key for a resolution, canonicalizing matchers, predicates, and the index
// set so input order does not change it.
//
// A window is not immutable (late data lands in past windows), but each index object is. Keying on the
// listed set plus the exact [start, end] nanos means late data changes the set and the key, so a stale
// entry is never served, and adjacent windows sharing a straddling index object never alias. The key is
// a hash, so inputs are stored with the value and re-checked on read.
func sectionsCacheKey(tenant string, req SectionsRequest, indexes []IndexEntry) string {
	return hashSectionsCacheKey(
		tenant,
		req.Start.UnixNano(),
		req.End.UnixNano(),
		stableMatchers(req.Matchers),
		stableMatchers(req.Predicates),
		stableIndexEntries(indexes),
	)
}

func hashSectionsCacheKey(tenant string, startNanos, endNanos int64, matchers, predicates string, indexes []IndexEntry) string {
	h := xxhash.New()
	_, _ = h.WriteString(tenant)
	_, _ = h.WriteString("\x00s")
	_, _ = h.WriteString(strconv.FormatInt(startNanos, 10))
	_, _ = h.WriteString("\x00e")
	_, _ = h.WriteString(strconv.FormatInt(endNanos, 10))
	_, _ = h.WriteString("\x00m")
	_, _ = h.WriteString(matchers)
	_, _ = h.WriteString("\x00p")
	_, _ = h.WriteString(predicates)
	_, _ = h.WriteString("\x00i")
	for _, idx := range indexes {
		_, _ = h.WriteString(idx.Path)
		_, _ = h.WriteString("\x00")
	}
	return sectionsCacheKeyPrefix + sectionsCacheKeyVersion + ":" + tenant + ":" + strconv.FormatUint(h.Sum64(), 16)
}

// stableMatchers returns an order-independent string form of the matchers, so selectors written in a
// different order key the same entry. Empty matchers stringify to "{}".
func stableMatchers(matchers []*labels.Matcher) string {
	sorted := append([]*labels.Matcher(nil), matchers...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		if sorted[i].Type != sorted[j].Type {
			return sorted[i].Type < sorted[j].Type
		}
		return sorted[i].Value < sorted[j].Value
	})
	return matchersToString(sorted)
}

// stableIndexEntries returns a copy of indexes sorted stably by (path, start, end), so the input order
// does not affect the key or the collision check.
func stableIndexEntries(indexes []IndexEntry) []IndexEntry {
	out := append([]IndexEntry(nil), indexes...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		return out[i].End.Before(out[j].End)
	})
	return out
}

func toCachedIndexEntries(indexes []IndexEntry) []*CachedIndexEntry {
	out := make([]*CachedIndexEntry, len(indexes))
	for i, idx := range indexes {
		out[i] = &CachedIndexEntry{
			Path:       idx.Path,
			StartNanos: idx.Start.UnixNano(),
			EndNanos:   idx.End.UnixNano(),
		}
	}
	return out
}

// cachedIndexesMatch reports whether the cached index set equals the requested one. Both sides are
// stable-sorted before store and compare, so an ordered comparison is exact; a mismatch is a collision
// and re-resolves.
func cachedIndexesMatch(cached []*CachedIndexEntry, indexes []IndexEntry) bool {
	if len(cached) != len(indexes) {
		return false
	}
	for i := range indexes {
		if cached[i].Path != indexes[i].Path ||
			cached[i].StartNanos != indexes[i].Start.UnixNano() ||
			cached[i].EndNanos != indexes[i].End.UnixNano() {
			return false
		}
	}
	return true
}

func toCachedSectionDescriptors(sections []*DataobjSectionDescriptor) []*CachedSectionDescriptor {
	out := make([]*CachedSectionDescriptor, len(sections))
	for i, s := range sections {
		out[i] = &CachedSectionDescriptor{
			ObjectPath:          s.ObjectPath,
			SectionIdx:          s.SectionIdx,
			StreamIds:           s.StreamIDs,
			RowCount:            int64(s.RowCount),
			Size_:               s.Size,
			StartNanos:          s.Start.UnixNano(),
			EndNanos:            s.End.UnixNano(),
			AmbiguousPredicates: s.AmbiguousPredicates,
		}
	}
	return out
}

func sectionsFromCached(cached []*CachedSectionDescriptor) []*DataobjSectionDescriptor {
	out := make([]*DataobjSectionDescriptor, len(cached))
	for i, s := range cached {
		out[i] = &DataobjSectionDescriptor{
			SectionKey: SectionKey{
				ObjectPath: s.ObjectPath,
				SectionIdx: s.SectionIdx,
			},
			StreamIDs:           s.StreamIds,
			RowCount:            int(s.RowCount),
			Size:                s.Size_,
			Start:               time.Unix(0, s.StartNanos).UTC(),
			End:                 time.Unix(0, s.EndNanos).UTC(),
			AmbiguousPredicates: s.AmbiguousPredicates,
		}
	}
	return out
}
