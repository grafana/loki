package hintprovider

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

type opaqueState struct{ id string }

// sized returns an entry whose size estimate is n bytes.
func sized(id string, n uint64) cachedMetadata {
	return cachedMetadata{
		headerInfo: format.HeaderInfo{DocMetadataSize: n},
		state:      &opaqueState{id: id},
	}
}

func newTestMetadataCache(t *testing.T, maxEntries int, maxBytes uint64) *metadataCache {
	t.Helper()
	c, err := newMetadataCache(maxEntries, maxBytes, prometheus.NewPedanticRegistry())
	require.NoError(t, err)
	return c
}

// requireCached checks the cache holds exactly ids, and that the entry and
// byte gauges agree with the cache.
func requireCached(t *testing.T, c *metadataCache, wantBytes uint64, ids ...string) {
	t.Helper()
	require.Equal(t, len(ids), c.len())
	for _, id := range ids {
		_, ok := c.lru.Peek(id)
		require.True(t, ok, "%s should be cached", id)
	}
	require.Equal(t, wantBytes, c.bytes)
	require.Equal(t, float64(len(ids)), testutil.ToFloat64(c.metrics.entries))
	require.Equal(t, float64(wantBytes), testutil.ToFloat64(c.metrics.sizeBytes))
}

func TestMetadataCache_EntryCapEvictsLeastRecentlyUsed(t *testing.T) {
	c := newTestMetadataCache(t, 2, 1<<20)

	c.put("a", sized("a", 10))
	c.put("b", sized("b", 20))
	_, ok := c.get("a") // b is now the least recently used
	require.True(t, ok)
	c.put("c", sized("c", 30))

	requireCached(t, c, 40, "a", "c")
	require.Equal(t, 1.0, testutil.ToFloat64(c.metrics.evictions))
	require.Zero(t, testutil.ToFloat64(c.metrics.drops))
}

func TestMetadataCache_ByteBudgetEvictsLeastRecentlyUsed(t *testing.T) {
	c := newTestMetadataCache(t, 100, 100)

	c.put("a", sized("a", 40))
	c.put("b", sized("b", 40))
	c.put("c", sized("c", 10))
	_, ok := c.get("a") // oldest first: b, c, a
	require.True(t, ok)

	// 90 + 50 is over the budget, and evicting b brings it back to 100.
	c.put("d", sized("d", 50))
	requireCached(t, c, 100, "a", "c", "d")
	require.Equal(t, 1.0, testutil.ToFloat64(c.metrics.evictions))

	// 100 + 60 takes three evictions, oldest first: c, a, then d.
	c.put("e", sized("e", 60))
	requireCached(t, c, 60, "e")
	require.Equal(t, 4.0, testutil.ToFloat64(c.metrics.evictions))
	require.Zero(t, testutil.ToFloat64(c.metrics.drops))
}

func TestMetadataCache_DropsEntryLargerThanBudget(t *testing.T) {
	c := newTestMetadataCache(t, 100, 100)
	c.put("a", sized("a", 40))
	c.put("b", sized("b", 40))

	c.put("big", sized("big", 101))
	requireCached(t, c, 80, "a", "b")
	require.Equal(t, 1.0, testutil.ToFloat64(c.metrics.drops))
	require.Zero(t, testutil.ToFloat64(c.metrics.evictions))

	// Replacing a cached entry with one too large removes the old entry
	// rather than keeping metadata the caller has replaced.
	c.put("a", sized("a", 101))
	requireCached(t, c, 40, "b")
	require.Equal(t, 2.0, testutil.ToFloat64(c.metrics.drops))
}

func TestMetadataCache_ReplaceUpdatesSize(t *testing.T) {
	c := newTestMetadataCache(t, 100, 100)
	c.put("a", sized("a", 40))
	c.put("b", sized("b", 40))

	replacement := sized("a", 10)
	c.put("a", replacement)
	requireCached(t, c, 50, "a", "b")
	got, ok := c.get("a")
	require.True(t, ok)
	require.Same(t, replacement.state, got.state)

	// Growing an entry past the budget evicts the others, oldest first, and
	// keeps the entry that grew.
	c.put("a", sized("a", 90))
	requireCached(t, c, 90, "a")
	require.Equal(t, 1.0, testutil.ToFloat64(c.metrics.evictions))
}

func TestMetadataCache_Delete(t *testing.T) {
	c := newTestMetadataCache(t, 100, 100)
	c.put("a", sized("a", 40))
	c.put("b", sized("b", 30))

	c.delete("a")
	requireCached(t, c, 30, "b")
	c.delete("missing")
	requireCached(t, c, 30, "b")
	require.Zero(t, testutil.ToFloat64(c.metrics.evictions), "a delete is not an eviction")
}

func TestMetadataCache_IgnoresNilState(t *testing.T) {
	c := newTestMetadataCache(t, 100, 100)
	c.put("a", cachedMetadata{headerInfo: format.HeaderInfo{DocMetadataSize: 10}})
	requireCached(t, c, 0)
}

func TestMetadataCache_EntriesWithoutSizeAreBoundedByCount(t *testing.T) {
	c := newTestMetadataCache(t, 2, 100)
	c.put("a", sized("a", 0))
	c.put("b", sized("b", 0))
	c.put("c", sized("c", 0))
	requireCached(t, c, 0, "b", "c")
}

func TestNewMetadataCache_RejectsNonPositiveEntryCap(t *testing.T) {
	_, err := newMetadataCache(0, 100, nil)
	require.Error(t, err)
}
