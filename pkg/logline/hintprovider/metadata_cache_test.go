package hintprovider

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

type opaqueState struct{}

// entryOfSize returns metadata whose estimated size is n bytes.
func entryOfSize(n uint64) cachedMetadata {
	return cachedMetadata{
		headerInfo: format.HeaderInfo{DocMetadataSize: n},
		state:      &opaqueState{},
	}
}

func newTestCache(t *testing.T, maxEntries int, maxBytes uint64) (*metadataCache, *cacheMetrics) {
	t.Helper()
	c := newMetadataCache(maxEntries, maxBytes, prometheus.NewRegistry())
	return c, c.metrics
}

func requireCached(t *testing.T, c *metadataCache, present bool, ids ...string) {
	t.Helper()
	for _, id := range ids {
		c.mu.Lock()
		_, ok := c.entries[id]
		c.mu.Unlock()
		require.Equal(t, present, ok, id)
	}
}

func TestMetadataCache_ByteBudgetEvictsLRU(t *testing.T) {
	c, m := newTestCache(t, 100, 30)
	c.put("a", entryOfSize(10))
	c.put("b", entryOfSize(10))
	c.put("c", entryOfSize(10))
	require.Equal(t, uint64(30), c.estimatedSize)

	c.put("d", entryOfSize(10))
	requireCached(t, c, false, "a")
	requireCached(t, c, true, "b", "c", "d")
	require.Equal(t, uint64(30), c.estimatedSize)

	// A larger insert evicts several, oldest first.
	c.put("e", entryOfSize(25))
	requireCached(t, c, false, "b", "c", "d")
	requireCached(t, c, true, "e")
	require.Equal(t, uint64(25), c.estimatedSize)

	require.Equal(t, float64(4), testutil.ToFloat64(m.evictions))
	require.Equal(t, float64(0), testutil.ToFloat64(m.drops))
	require.Equal(t, float64(1), testutil.ToFloat64(m.entries))
	require.Equal(t, float64(25), testutil.ToFloat64(m.sizeBytes))
}

func TestMetadataCache_GetRefreshesRecency(t *testing.T) {
	c, _ := newTestCache(t, 100, 30)
	c.put("a", entryOfSize(10))
	c.put("b", entryOfSize(10))
	c.put("c", entryOfSize(10))

	_, ok := c.get("a")
	require.True(t, ok)
	c.put("d", entryOfSize(10))

	requireCached(t, c, true, "a", "c", "d")
	requireCached(t, c, false, "b")
}

func TestMetadataCache_OversizeEntryDropped(t *testing.T) {
	c, m := newTestCache(t, 100, 30)
	c.put("a", entryOfSize(10))
	c.put("big", entryOfSize(31))

	requireCached(t, c, false, "big")
	requireCached(t, c, true, "a")
	require.Equal(t, uint64(10), c.estimatedSize)
	require.Equal(t, float64(1), testutil.ToFloat64(m.drops))
	require.Equal(t, float64(0), testutil.ToFloat64(m.evictions))

	// An update that grows past the budget removes the stale entry.
	c.put("a", entryOfSize(31))
	requireCached(t, c, false, "a")
	require.Equal(t, uint64(0), c.estimatedSize)
	require.Equal(t, float64(2), testutil.ToFloat64(m.drops))
}

func TestMetadataCache_EntryCapStillEnforced(t *testing.T) {
	c, m := newTestCache(t, 2, 1000)
	c.put("a", entryOfSize(1))
	c.put("b", entryOfSize(1))
	c.put("c", entryOfSize(1))

	require.Equal(t, 2, c.len())
	requireCached(t, c, false, "a")
	requireCached(t, c, true, "b", "c")
	require.Equal(t, float64(1), testutil.ToFloat64(m.evictions))
}

func TestMetadataCache_NoByteBudget(t *testing.T) {
	c, _ := newTestCache(t, 3, 0)
	c.put("a", entryOfSize(1<<40))
	c.put("b", entryOfSize(1<<40))
	require.Equal(t, 2, c.len())
}

func TestMetadataCache_UpdateAndDeleteKeepSizeInSync(t *testing.T) {
	c, m := newTestCache(t, 10, 100)
	c.put("a", entryOfSize(10))
	c.put("b", entryOfSize(20))
	c.put("a", entryOfSize(40))
	require.Equal(t, uint64(60), c.estimatedSize)
	require.Equal(t, 2, c.len())

	c.delete("b")
	c.delete("missing")
	require.Equal(t, uint64(40), c.estimatedSize)
	require.Equal(t, float64(40), testutil.ToFloat64(m.sizeBytes))
	require.Equal(t, float64(1), testutil.ToFloat64(m.entries))
}

func TestMetadataCache_NilStateIgnored(t *testing.T) {
	c, _ := newTestCache(t, 10, 100)
	c.put("a", cachedMetadata{})
	require.Equal(t, 0, c.len())
}
