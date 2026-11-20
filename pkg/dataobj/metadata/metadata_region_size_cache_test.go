package metadata

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestMetadataRegionSizeCache_get(t *testing.T) {
	t.Run("returns the size that set stored", func(t *testing.T) {
		r := newTestMetadataRegionSizeCache(1<<20, &fakeClock{now: time.Unix(1000, 0)})
		r.set("a", 1)

		requireSize(t, r, "a", 1)
	})

	t.Run("reports a miss for a path that nobody set", func(t *testing.T) {
		r := newTestMetadataRegionSizeCache(1<<20, &fakeClock{now: time.Unix(1000, 0)})
		r.set("a", 1)

		requireNoSize(t, r, "b")
	})

	t.Run("counts the hits and the misses", func(t *testing.T) {
		r := newTestMetadataRegionSizeCache(1<<20, &fakeClock{now: time.Unix(1000, 0)})
		r.set("a", 1)

		requireSize(t, r, "a", 1)
		requireSize(t, r, "a", 1)
		requireNoSize(t, r, "b")

		require.Equal(t, 2.0, testutil.ToFloat64(r.hits))
		require.Equal(t, 1.0, testutil.ToFloat64(r.misses))
	})

	t.Run("drops the entries that nothing used for 24 hours and keeps the ones that a get used since", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(1000, 0)}
		r := newTestMetadataRegionSizeCache(1<<20, clock)
		r.set("used", 1)
		r.set("idle", 2)
		r.set("idle too", 3)

		clock.now = clock.now.Add(20 * time.Hour)
		requireSize(t, r, "used", 1)
		clock.now = clock.now.Add(10 * time.Hour)

		requireSize(t, r, "used", 1)
		requireNoSize(t, r, "idle")
		requireNoSize(t, r, "idle too")
		require.Equal(t, 2.0, testutil.ToFloat64(r.evictedByIdle))
	})

	t.Run("finds nothing in a nil cache", func(t *testing.T) {
		var r *metadataRegionSizeCache

		requireNoSize(t, r, "a")
	})
}

func TestMetadataRegionSizeCache_set(t *testing.T) {
	t.Run("drops the entry that was used least recently when the bytes exceed the limit", func(t *testing.T) {
		r := newTestMetadataRegionSizeCache(2*entryBytes("a"), &fakeClock{now: time.Unix(1000, 0)})
		r.set("a", 1)
		r.set("b", 2)
		requireSize(t, r, "a", 1)

		r.set("c", 3)

		requireSize(t, r, "a", 1)
		requireSize(t, r, "c", 3)
		requireNoSize(t, r, "b")
		require.Equal(t, 1.0, testutil.ToFloat64(r.evictedBySize))
	})

	t.Run("replaces the size and counts the bytes of a path once when it is set twice", func(t *testing.T) {
		r := newTestMetadataRegionSizeCache(2*entryBytes("a"), &fakeClock{now: time.Unix(1000, 0)})
		r.set("a", 1)
		r.set("a", 5)

		r.set("b", 2)

		requireSize(t, r, "a", 5)
		requireSize(t, r, "b", 2)
	})

	t.Run("ignores a size that is not positive", func(t *testing.T) {
		r := newTestMetadataRegionSizeCache(1<<20, &fakeClock{now: time.Unix(1000, 0)})

		r.set("a", 0)
		r.set("b", -1)

		requireNoSize(t, r, "a")
		requireNoSize(t, r, "b")
	})

	t.Run("ignores an entry that is larger than the whole cache and keeps the other entries", func(t *testing.T) {
		r := newTestMetadataRegionSizeCache(entryBytes("a"), &fakeClock{now: time.Unix(1000, 0)})
		r.set("a", 1)

		r.set("a long path", 2)

		requireSize(t, r, "a", 1)
		requireNoSize(t, r, "a long path")
	})

	t.Run("does nothing in a nil cache", func(t *testing.T) {
		var r *metadataRegionSizeCache

		r.set("a", 1)

		requireNoSize(t, r, "a")
	})
}

func TestMetadataRegionSizeEntryOverheadBytes(t *testing.T) {
	t.Run("estimates the heap bytes of an entry within 25 percent", func(t *testing.T) {
		heapBytes := func() uint64 {
			runtime.GC()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			return m.HeapAlloc
		}

		const entries = 20_000
		paths := make([]string, entries)
		for i := range paths {
			paths[i] = fmt.Sprintf("objects/%02x/%062x", i%256, i)
		}
		r := newMetadataRegionSizeCache(1<<40, time.Now, nil)

		before := heapBytes()
		for i, path := range paths {
			r.set(path, int64(i+1))
		}
		after := heapBytes()
		runtime.KeepAlive(r)

		// The allocator rounds objects up and the map grows in steps, so the estimate is only close. The
		// tolerance still catches an estimate that is off by a wrong order of magnitude.
		require.InEpsilon(t, float64(entryBytes(paths[0])), float64(after-before)/entries, 0.25)
	})
}

func newTestMetadataRegionSizeCache(maxBytes int64, clock *fakeClock) *metadataRegionSizeCache {
	return newMetadataRegionSizeCache(maxBytes, clock.Now, nil)
}

func requireSize(t *testing.T, r *metadataRegionSizeCache, path string, want int64) {
	t.Helper()

	got, ok := r.get(path)
	require.True(t, ok, path)
	require.Equal(t, want, got, path)
}

func requireNoSize(t *testing.T, r *metadataRegionSizeCache, path string) {
	t.Helper()

	_, ok := r.get(path)
	require.False(t, ok, path)
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }
