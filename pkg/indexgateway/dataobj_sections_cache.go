package indexgateway

import (
	"context"
	"sort"
	"strconv"

	"github.com/cespare/xxhash/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// dataObjectSectionsCacheKeyPrefix namespaces every key so these entries never collide with other
// subsystems' keys in a shared cache (e.g. a memcached cluster used by several caches). The trailing
// slash keeps the namespace distinct from any key that merely starts with the same letters.
const dataObjectSectionsCacheKeyPrefix = "dataobj-sections/"

// dataObjectSectionsCacheKeyVersion follows the prefix in every cache key. Bump it when the resolution
// logic or the cached-value encoding changes in a way that makes old entries wrong.
const dataObjectSectionsCacheKeyVersion = "v1"

// dataObjectSectionsCache stores window resolutions in a cache.Cache. It works with high-level types:
// callers pass the resolution inputs (tenant, matchers, index-object set) and the response, and the
// cache handles keying, serialization, and collision detection.
//
// The underlying cache is whatever cache.New builds: typically a tiered embedded (L1) + memcached
// (L2) cache, where memcached writes are already asynchronous (background write-back).
type dataObjectSectionsCache struct {
	services.Service

	cache  cache.Cache
	logger log.Logger
}

func newDataObjectSectionsCache(c cache.Cache, logger log.Logger) *dataObjectSectionsCache {
	s := &dataObjectSectionsCache{cache: c, logger: logger}
	// The underlying cache is released when the service stops.
	s.Service = services.NewIdleService(nil, func(_ error) error {
		if s.cache != nil {
			s.cache.Stop()
		}
		return nil
	})
	return s
}

// get returns the cached response for (tenant, matchers, indexes), or false on a miss, a cache
// error, an undecodable entry, or a key collision (the stored inputs do not match the requested
// ones). Any of these degrades to recompute rather than failing or returning a wrong answer.
func (c *dataObjectSectionsCache) get(ctx context.Context, tenant string, from model.Time, matchers string, indexes []metastore.IndexEntry) (*logproto.ResolveDataObjectSectionsResponse, bool) {
	if c.cache == nil {
		return nil, false
	}
	// Canonicalize so the input order of matchers and index entries does not change the key or the
	// collision check.
	matchers = stableMatchers(matchers)
	indexes = stableIndexEntries(indexes)
	windowFromNanos := from.Time().UnixNano()
	key := dataObjectSectionsCacheKey(tenant, windowFromNanos, matchers, indexes)

	_, bufs, _, err := c.cache.Fetch(ctx, []string{key})
	if err != nil {
		level.Warn(c.logger).Log("msg", "data object sections cache fetch failed", "err", err)
		return nil, false
	}
	if len(bufs) != 1 {
		return nil, false
	}

	var entry logproto.CachedDataObjectSections
	if err := entry.Unmarshal(bufs[0]); err != nil {
		level.Warn(c.logger).Log("msg", "discarding undecodable cached resolve response", "err", err)
		return nil, false
	}
	if entry.Matchers != matchers || entry.WindowFromNanos != windowFromNanos || !cachedDataObjectIndexesMatch(entry.Indexes, indexes) {
		level.Warn(c.logger).Log("msg", "data object sections cache key collision; treating as miss", "key", key)
		return nil, false
	}
	return &logproto.ResolveDataObjectSectionsResponse{Objects: entry.Objects}, true
}

// put stores resp for (tenant, matchers, indexes) together with those inputs (for collision
// detection on read). Writes to the shared (memcached) layer are asynchronous, so put does not block
// the caller on it.
func (c *dataObjectSectionsCache) put(ctx context.Context, tenant string, from model.Time, matchers string, indexes []metastore.IndexEntry, resp *logproto.ResolveDataObjectSectionsResponse) {
	if c.cache == nil {
		return
	}

	// Store the canonical form so a later get with a different input order still matches.
	matchers = stableMatchers(matchers)
	indexes = stableIndexEntries(indexes)
	windowFromNanos := from.Time().UnixNano()
	key := dataObjectSectionsCacheKey(tenant, windowFromNanos, matchers, indexes)

	entry := logproto.CachedDataObjectSections{
		Matchers:        matchers,
		Indexes:         toCachedDataObjectIndexEntries(indexes),
		Objects:         resp.Objects,
		WindowFromNanos: windowFromNanos,
	}
	b, err := entry.Marshal()
	if err != nil {
		level.Warn(c.logger).Log("msg", "marshalling resolve response for cache failed", "err", err)
		return
	}
	if err := c.cache.Store(ctx, []string{key}, [][]byte{b}); err != nil {
		level.Warn(c.logger).Log("msg", "data object sections cache store failed", "err", err)
	}
}

func toCachedDataObjectIndexEntries(indexes []metastore.IndexEntry) []logproto.CachedDataObjectIndexEntry {
	out := make([]logproto.CachedDataObjectIndexEntry, len(indexes))
	for i, idx := range indexes {
		out[i] = logproto.CachedDataObjectIndexEntry{
			Path:       idx.Path,
			StartNanos: idx.Start.UnixNano(),
			EndNanos:   idx.End.UnixNano(),
		}
	}
	return out
}

// cachedDataObjectIndexesMatch reports whether the cached index set equals the requested one. Both
// sides are stable-sorted (stableIndexEntries) before store and compare, so an ordered comparison is
// exact; a mismatch is treated as a collision and re-resolved.
func cachedDataObjectIndexesMatch(cached []logproto.CachedDataObjectIndexEntry, indexes []metastore.IndexEntry) bool {
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

// dataObjectSectionsCacheKey derives the cache key for a window resolution.
//
// A 12h window is not immutable: data arrives late and new index objects land in past windows. A
// data object is immutable, though, so the key is derived from the immutable set of index objects
// listed for the window plus the matchers. Late data adds an index object, which changes the set and
// therefore the key, so a stale entry is never served. The result is a hash, so the value it keys is
// stored with its inputs and re-checked on read to guard against collisions.
//
// The window start is part of the key. Resolution time-filters streams by the window, but the listed
// index-object set can be identical for two adjacent windows that share a straddling object, so the
// object set alone would alias the two windows onto one entry.
//
// It expects canonical inputs: matchers from [stableMatchers] and index entries from
// [stableIndexEntries], so the input order does not affect the key.
func dataObjectSectionsCacheKey(tenant string, windowFromNanos int64, matchers string, indexes []metastore.IndexEntry) string {
	h := xxhash.New()
	_, _ = h.WriteString(tenant)
	_, _ = h.WriteString("\x00w")
	_, _ = h.WriteString(strconv.FormatInt(windowFromNanos, 10))
	_, _ = h.WriteString("\x00m")
	_, _ = h.WriteString(matchers)
	_, _ = h.WriteString("\x00i")
	for _, idx := range indexes {
		_, _ = h.WriteString(idx.Path)
		_, _ = h.WriteString("\x00")
	}

	return dataObjectSectionsCacheKeyPrefix + dataObjectSectionsCacheKeyVersion + ":" + tenant + ":" + strconv.FormatUint(h.Sum64(), 16)
}

// stableMatchers returns a stable, order-independent string form of the matchers: it parses,
// sorts, and re-stringifies them, so semantically equal selectors written in a different order key
// the same entry. On a parse error it returns the input unchanged (the caller has already validated
// it, so this is defensive).
func stableMatchers(matchers string) string {
	parsed, err := syntax.ParseMatchers(matchers, true)
	if err != nil {
		return matchers
	}
	sort.Slice(parsed, func(i, j int) bool {
		if parsed[i].Name != parsed[j].Name {
			return parsed[i].Name < parsed[j].Name
		}
		if parsed[i].Type != parsed[j].Type {
			return parsed[i].Type < parsed[j].Type
		}
		return parsed[i].Value < parsed[j].Value
	})
	return syntax.MatchersString(parsed)
}

// stableIndexEntries returns a copy of indexes sorted stably by (path, start, end), so the input
// order does not affect the key or the collision check.
func stableIndexEntries(indexes []metastore.IndexEntry) []metastore.IndexEntry {
	out := append([]metastore.IndexEntry(nil), indexes...)
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
