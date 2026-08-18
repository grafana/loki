package indexgateway

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

func newTestEmbeddedCache(t *testing.T, name string) *cache.EmbeddedCache[string, []byte] {
	t.Helper()
	c := cache.NewEmbeddedCache(name, cache.EmbeddedCacheConfig{
		Enabled:      true,
		MaxSizeItems: 100,
		TTL:          time.Hour,
	}, nil, log.NewNopLogger(), stats.CacheType("test"))
	t.Cleanup(c.Stop)
	return c
}

func testIndexes() []metastore.IndexEntry {
	return []metastore.IndexEntry{{Path: "a", Start: time.Unix(0, 0), End: time.Unix(100, 0)}}
}

func testResponse() *logproto.ResolveDataObjectSectionsResponse {
	return &logproto.ResolveDataObjectSectionsResponse{Objects: []logproto.ResolvedDataObject{
		{ObjectPath: "obj", Sections: []logproto.ResolvedDataObjectSection{{SectionIdx: 0, StreamIds: []int64{1, 2}}}},
	}}
}

func TestDataObjectSectionsCache(t *testing.T) {
	ctx := context.Background()
	matchers := `{app="foo"}`
	indexes := testIndexes()
	from, _ := resolverTestWindow()
	fromNanos := from.Time().UnixNano()

	t.Run("put then get roundtrip", func(t *testing.T) {
		c := newDataObjectSectionsCache(newTestEmbeddedCache(t, "roundtrip"), log.NewNopLogger())
		c.put(ctx, "tenant", from, matchers, indexes, testResponse())
		got, ok := c.get(ctx, "tenant", from, matchers, indexes)
		require.True(t, ok)
		require.True(t, testResponse().Equal(got))
	})

	t.Run("miss returns false", func(t *testing.T) {
		c := newDataObjectSectionsCache(newTestEmbeddedCache(t, "miss"), log.NewNopLogger())
		_, ok := c.get(ctx, "tenant", from, matchers, indexes)
		require.False(t, ok)
	})

	t.Run("cache error degrades to miss, not failure", func(t *testing.T) {
		c := newDataObjectSectionsCache(erroringCache{}, log.NewNopLogger())
		_, ok := c.get(ctx, "tenant", from, matchers, indexes) // Fetch errors -> treated as miss
		require.False(t, ok)
		c.put(ctx, "tenant", from, matchers, indexes, testResponse()) // Store errors -> must not panic
	})

	t.Run("undecodable entry degrades to miss", func(t *testing.T) {
		c := newDataObjectSectionsCache(fixedCache{value: []byte("not-a-proto")}, log.NewNopLogger())
		_, ok := c.get(ctx, "tenant", from, matchers, indexes)
		require.False(t, ok)
	})

	t.Run("key collision detected via stored inputs", func(t *testing.T) {
		// An entry whose stored matchers differ from the requested ones simulates a hash collision.
		entry := logproto.CachedDataObjectSections{
			Matchers:        `{app="OTHER"}`,
			Indexes:         toCachedDataObjectIndexEntries(indexes),
			Objects:         testResponse().Objects,
			WindowFromNanos: fromNanos,
		}
		b, err := entry.Marshal()
		require.NoError(t, err)

		c := newDataObjectSectionsCache(fixedCache{value: b}, log.NewNopLogger())
		_, ok := c.get(ctx, "tenant", from, matchers, indexes) // requested matchers != stored -> collision -> miss
		require.False(t, ok)
	})

	t.Run("collision detected on differing index set", func(t *testing.T) {
		entry := logproto.CachedDataObjectSections{
			Matchers:        matchers,
			Indexes:         toCachedDataObjectIndexEntries([]metastore.IndexEntry{{Path: "different", Start: time.Unix(0, 0), End: time.Unix(100, 0)}}),
			Objects:         testResponse().Objects,
			WindowFromNanos: fromNanos,
		}
		b, err := entry.Marshal()
		require.NoError(t, err)

		c := newDataObjectSectionsCache(fixedCache{value: b}, log.NewNopLogger())
		_, ok := c.get(ctx, "tenant", from, matchers, indexes)
		require.False(t, ok)
	})

	t.Run("collision detected on differing window", func(t *testing.T) {
		// Same matchers and index set, but the stored entry is for a different window: two adjacent
		// windows can list the same straddling object, so the window must be re-checked on read.
		entry := logproto.CachedDataObjectSections{
			Matchers:        matchers,
			Indexes:         toCachedDataObjectIndexEntries(stableIndexEntries(indexes)),
			Objects:         testResponse().Objects,
			WindowFromNanos: from.Add(metastore.MetastoreWindowSize).Time().UnixNano(),
		}
		b, err := entry.Marshal()
		require.NoError(t, err)

		c := newDataObjectSectionsCache(fixedCache{value: b}, log.NewNopLogger())
		_, ok := c.get(ctx, "tenant", from, matchers, indexes)
		require.False(t, ok)
	})

	t.Run("nil cache is safe", func(t *testing.T) {
		c := newDataObjectSectionsCache(nil, log.NewNopLogger())
		c.put(ctx, "tenant", from, matchers, indexes, testResponse())
		_, ok := c.get(ctx, "tenant", from, matchers, indexes)
		require.False(t, ok)
	})

	t.Run("different window does not hit", func(t *testing.T) {
		c := newDataObjectSectionsCache(newTestEmbeddedCache(t, "window"), log.NewNopLogger())
		c.put(ctx, "tenant", from, matchers, indexes, testResponse())

		// A neighbouring window with the same matchers and index set must not read the first's entry.
		_, ok := c.get(ctx, "tenant", from.Add(metastore.MetastoreWindowSize), matchers, indexes)
		require.False(t, ok)
	})

	t.Run("hit regardless of matcher and index-entry order", func(t *testing.T) {
		c := newDataObjectSectionsCache(newTestEmbeddedCache(t, "order"), log.NewNopLogger())
		idxA := []metastore.IndexEntry{
			{Path: "a", Start: time.Unix(0, 0), End: time.Unix(100, 0)},
			{Path: "b", Start: time.Unix(0, 0), End: time.Unix(100, 0)},
		}
		idxB := []metastore.IndexEntry{idxA[1], idxA[0]} // reversed

		c.put(ctx, "tenant", from, `{app="foo", level="error"}`, idxA, testResponse())

		// Different matcher order + different index-entry order must still hit.
		got, ok := c.get(ctx, "tenant", from, `{level="error", app="foo"}`, idxB)
		require.True(t, ok)
		require.True(t, testResponse().Equal(got))
	})
}

func TestDataObjectSectionsCacheKey(t *testing.T) {
	// The key expects canonical inputs (stableMatchers / stableIndexEntries), so tests pass them.
	idx := stableIndexEntries([]metastore.IndexEntry{{Path: "a"}, {Path: "b"}})
	from, _ := resolverTestWindow()
	fromNanos := from.Time().UnixNano()
	base := dataObjectSectionsCacheKey("tenant", fromNanos, stableMatchers(`{app="foo"}`), idx)

	t.Run("deterministic", func(t *testing.T) {
		require.Equal(t, base, dataObjectSectionsCacheKey("tenant", fromNanos, stableMatchers(`{app="foo"}`), idx))
	})

	t.Run("is namespaced with a fixed prefix", func(t *testing.T) {
		require.True(t, strings.HasPrefix(base, "dataobj-sections/"), "key %q must be namespaced to avoid collisions in a shared cache", base)
	})

	t.Run("changes with the index-object set (late data)", func(t *testing.T) {
		withNew := stableIndexEntries(append(append([]metastore.IndexEntry(nil), idx...), metastore.IndexEntry{Path: "c"}))
		require.NotEqual(t, base, dataObjectSectionsCacheKey("tenant", fromNanos, stableMatchers(`{app="foo"}`), withNew))
	})

	t.Run("changes with tenant", func(t *testing.T) {
		require.NotEqual(t, base, dataObjectSectionsCacheKey("other", fromNanos, stableMatchers(`{app="foo"}`), idx))
	})

	t.Run("changes with matchers", func(t *testing.T) {
		require.NotEqual(t, base, dataObjectSectionsCacheKey("tenant", fromNanos, stableMatchers(`{app="bar"}`), idx))
	})

	t.Run("changes with window (from)", func(t *testing.T) {
		otherFromNanos := from.Add(metastore.MetastoreWindowSize).Time().UnixNano()
		require.NotEqual(t, base, dataObjectSectionsCacheKey("tenant", otherFromNanos, stableMatchers(`{app="foo"}`), idx))
	})

	t.Run("no field-boundary collision", func(t *testing.T) {
		require.NotEqual(t,
			dataObjectSectionsCacheKey("ten", fromNanos, "ant", idx),
			dataObjectSectionsCacheKey("tenant", fromNanos, "", idx),
		)
	})
}

func TestStableMatchers(t *testing.T) {
	t.Run("order-independent and canonical", func(t *testing.T) {
		require.Equal(t, stableMatchers(`{app="foo", level="error"}`), stableMatchers(`{level="error", app="foo"}`))
	})

	t.Run("distinguishes different matchers", func(t *testing.T) {
		require.NotEqual(t, stableMatchers(`{app="foo"}`), stableMatchers(`{app="bar"}`))
	})

	t.Run("unparseable input returned unchanged", func(t *testing.T) {
		require.Equal(t, "not a selector", stableMatchers("not a selector"))
	})
}

func TestStableIndexEntries(t *testing.T) {
	a := metastore.IndexEntry{Path: "a", Start: time.Unix(0, 0), End: time.Unix(100, 0)}
	b := metastore.IndexEntry{Path: "b", Start: time.Unix(0, 0), End: time.Unix(100, 0)}
	require.Equal(t, stableIndexEntries([]metastore.IndexEntry{a, b}), stableIndexEntries([]metastore.IndexEntry{b, a}))
}

// erroringCache is a cache.Cache whose Fetch and Store always fail.
type erroringCache struct{}

func (erroringCache) Store(context.Context, []string, [][]byte) error { return errFail }
func (erroringCache) Fetch(context.Context, []string) ([]string, [][]byte, []string, error) {
	return nil, nil, nil, errFail
}
func (erroringCache) Stop()                         {}
func (erroringCache) GetCacheType() stats.CacheType { return stats.CacheType("test") }

var errFail = fmt.Errorf("cache boom")

// fixedCache is a cache.Cache that returns a fixed value for any key.
type fixedCache struct{ value []byte }

func (fixedCache) Store(context.Context, []string, [][]byte) error { return nil }
func (c fixedCache) Fetch(_ context.Context, keys []string) ([]string, [][]byte, []string, error) {
	return keys, [][]byte{c.value}, nil, nil
}
func (fixedCache) Stop()                         {}
func (fixedCache) GetCacheType() stats.CacheType { return stats.CacheType("test") }
