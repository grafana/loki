package metastore

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

// errBoom is a sentinel error for tests that only care that a bucket call fails.
var errBoom = errors.New("boom")

// erroringBucket fails every Get and Iter with a non-not-found error, so a refresh cannot list
// tenants for a window or load any path.
type erroringBucket struct {
	objstore.Bucket
	err error
}

func (b erroringBucket) Get(context.Context, string) (io.ReadCloser, error) { return nil, b.err }
func (b erroringBucket) IsObjNotFoundErr(error) bool                        { return false }

func (b erroringBucket) Iter(context.Context, string, func(string) error, ...objstore.IterOption) error {
	return b.err
}

// mapCache is an in-memory cache.Cache for tests.
type mapCache struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMapCache() *mapCache { return &mapCache{m: map[string][]byte{}} }

func (c *mapCache) Store(_ context.Context, keys []string, bufs [][]byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, k := range keys {
		c.m[k] = bufs[i]
	}
	return nil
}

func (c *mapCache) Fetch(_ context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range keys {
		if v, ok := c.m[k]; ok {
			found = append(found, k)
			bufs = append(bufs, v)
		} else {
			missing = append(missing, k)
		}
	}
	return found, bufs, missing, nil
}

func (c *mapCache) Stop() {}

// stopSpyCache records that Stop was called.
type stopSpyCache struct {
	cacheStore
	stopped atomic.Bool
}

func (c *stopSpyCache) Stop() { c.stopped.Store(true) }

func newWarmResolver(t *testing.T, bucket objstore.Bucket, cache cacheStore) *TableOfContentsWarmResolver {
	t.Helper()
	return newTableOfContentsWarmResolver(cache, bucket, 48*time.Hour, time.Minute, nil, log.NewNopLogger())
}

// seedTocEntry uploads path into tenant's ToC covering [ts-1h, ts+1h], so GetIndexes lists it for a
// query overlapping ts.
func seedTocEntry(t *testing.T, bucket objstore.Bucket, tenant, path string, ts time.Time) {
	t.Helper()
	ctx := user.InjectOrgID(context.Background(), tenant)
	w := NewTableOfContentsWriter(bucket, log.NewNopLogger())
	require.NoError(t, w.WriteEntry(ctx, tenant, TableOfContentsEntry{
		Path:      path,
		StartTime: ts.Add(-time.Hour),
		EndTime:   ts.Add(time.Hour),
	}))
}

func TestCachedToC_RoundTrip(t *testing.T) {
	entries := []IndexEntry{
		{Path: "obj-1", Start: time.Unix(100, 0).UTC(), End: time.Unix(200, 0).UTC()},
		{Path: "obj-2", Start: time.Unix(150, 0).UTC(), End: time.Unix(250, 0).UTC()},
	}
	b, err := encodeCachedToC(entries)
	require.NoError(t, err)
	got, err := decodeCachedToC(b)
	require.NoError(t, err)

	require.Len(t, got, len(entries))
	for i := range entries {
		require.Equal(t, entries[i].Path, got[i].Path)
		require.True(t, entries[i].Start.Equal(got[i].Start))
		require.True(t, entries[i].End.Equal(got[i].End))
	}
}

func TestCachedToC_RoundTrip_Empty(t *testing.T) {
	b, err := encodeCachedToC(nil)
	require.NoError(t, err)
	got, err := decodeCachedToC(b)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestWarmResolver_GetIndexes_WarmAndFallback(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	r := newWarmResolver(t, objstore.NewInMemBucket(), newMapCache())

	// A path warmed with two entries at disjoint times; a query overlaps only the first.
	snap := tocSnapshot{
		"warm-path": {
			{Path: "obj-early", Start: time.Unix(100, 0).UTC(), End: time.Unix(200, 0).UTC()},
			{Path: "obj-late", Start: time.Unix(1000, 0).UTC(), End: time.Unix(1100, 0).UTC()},
		},
	}
	r.snapshot.Store(&snap)

	t.Run("warm hit, time-filtered", func(t *testing.T) {
		got, err := r.GetIndexes(ctx, []string{"warm-path"}, time.Unix(150, 0).UTC(), time.Unix(160, 0).UTC())
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "obj-early", got[0].Path)
	})

	t.Run("cold path falls back to the lazy resolver", func(t *testing.T) {
		// A fresh resolver so the counters start at zero; it reuses the same warmed snapshot.
		r := newWarmResolver(t, objstore.NewInMemBucket(), newMapCache())
		r.snapshot.Store(&snap)
		// "cold-path" is absent from the snapshot; the lazy resolver reads it from the (empty) bucket,
		// which returns not-found and yields no entries.
		got, err := r.GetIndexes(ctx, []string{"warm-path", "cold-path"}, time.Unix(150, 0).UTC(), time.Unix(160, 0).UTC())
		require.NoError(t, err)
		require.Len(t, got, 1) // only the warm path matched
		require.Equal(t, 1.0, testutil.ToFloat64(r.source.WithLabelValues("cache")))
		require.Equal(t, 1.0, testutil.ToFloat64(r.source.WithLabelValues("storage")))
	})
}

func TestWarmResolver_GetIndexes_WarmEmptyPathIsNotCold(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	r := newWarmResolver(t, erroringBucket{err: errBoom}, newMapCache())

	// A warmed-but-empty path (a ToC the warmer confirmed has nothing for this query window) must be
	// served from the snapshot, not fall through to the erroring bucket.
	snap := tocSnapshot{"empty-path": {}}
	r.snapshot.Store(&snap)

	got, err := r.GetIndexes(ctx, []string{"empty-path"}, time.Unix(0, 0).UTC(), time.Unix(1000, 0).UTC())
	require.NoError(t, err)
	require.Empty(t, got)
	require.Equal(t, 1.0, testutil.ToFloat64(r.source.WithLabelValues("cache")))
}

func TestWarmResolver_Refresh_ReadsBucketThenDedupsViaCache(t *testing.T) {
	ts := now.Add(-2 * time.Hour)
	window := ts.Truncate(MetastoreWindowSize)
	bucket := objstore.NewInMemBucket()
	path := TableOfContentsPath(tenantID, window)
	seedTocEntry(t, bucket, tenantID, "src-obj", ts)

	shared := newMapCache() // shared across instances to exercise cross-instance dedup

	r1 := newWarmResolver(t, bucket, shared)
	r1.refresh(context.Background())
	require.Positive(t, testutil.ToFloat64(r1.objectStoreGets), "the first instance reads ToCs from object storage")

	snap1 := r1.snapshot.Load()
	require.NotNil(t, snap1)
	entries, ok := (*snap1)[path]
	require.True(t, ok, "the seeded tenant-window path must be warm")
	require.Len(t, entries, 1)
	require.Equal(t, "src-obj", entries[0].Path)

	// A second instance sharing the memcached layer must not re-read object storage for the same path.
	r2 := newWarmResolver(t, bucket, shared)
	r2.refresh(context.Background())
	require.Zero(t, testutil.ToFloat64(r2.objectStoreGets), "the second instance dedups via the shared cache")
	require.Positive(t, testutil.ToFloat64(r2.cacheHits))

	snap2 := r2.snapshot.Load()
	entries2, ok := (*snap2)[path]
	require.True(t, ok)
	require.Equal(t, entries, entries2)
}

func TestWarmResolver_Refresh_BucketErrorFallsBackToLazyForThatPath(t *testing.T) {
	ts := now.Add(-2 * time.Hour)
	window := ts.Truncate(MetastoreWindowSize)
	path := TableOfContentsPath(tenantID, window)

	good := objstore.NewInMemBucket()
	seedTocEntry(t, good, tenantID, "src-obj", ts)

	// Tenant listing (an Iter call) must still work so the refresh discovers this path; only the ToC
	// object's own Get fails, simulating a transient read failure after a successful listing.
	r := newWarmResolver(t, erroringBucket{Bucket: good, err: errBoom}, newMapCache())
	r.refresh(context.Background())

	snap := r.snapshot.Load()
	require.NotNil(t, snap)
	_, warm := (*snap)[path]
	require.False(t, warm, "a path whose load failed must be left out of the snapshot, not cached as empty")
	require.Positive(t, testutil.ToFloat64(r.refreshErrors))
}

func TestWarmResolver_Stopping_StopsTheCache(t *testing.T) {
	spy := &stopSpyCache{cacheStore: newMapCache()}
	r := newWarmResolver(t, objstore.NewInMemBucket(), spy)
	require.NoError(t, r.stopping(nil))
	require.True(t, spy.stopped.Load())
}

func TestTableOfContentsWarmResolverConfig_Validate(t *testing.T) {
	t.Run("rejects a non-positive refresh interval", func(t *testing.T) {
		cfg := TableOfContentsWarmResolverConfig{WarmWindow: time.Hour, RefreshInterval: 0}
		require.Error(t, cfg.Validate())
	})

	t.Run("rejects a non-positive warm window", func(t *testing.T) {
		cfg := TableOfContentsWarmResolverConfig{WarmWindow: 0, RefreshInterval: time.Minute}
		require.Error(t, cfg.Validate())
	})

	t.Run("accepts positive durations", func(t *testing.T) {
		cfg := TableOfContentsWarmResolverConfig{WarmWindow: time.Hour, RefreshInterval: time.Minute}
		require.NoError(t, cfg.Validate())
	})
}

func TestWarmResolver_NextRefreshDelay_NeverBelowInterval(t *testing.T) {
	r := newWarmResolver(t, objstore.NewInMemBucket(), newMapCache())
	r.refreshInterval = time.Minute
	for i := 0; i < 100; i++ {
		d := r.nextRefreshDelay()
		require.GreaterOrEqual(t, d, r.refreshInterval)
		require.Less(t, d, r.refreshInterval+r.refreshInterval/10+time.Millisecond)
	}
}
