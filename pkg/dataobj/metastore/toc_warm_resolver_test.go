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
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

// erroringBucket fails every Get with a non-not-found error, so a refresh cannot load any window.
type erroringBucket struct {
	objstore.Bucket
	err error
}

func (b erroringBucket) Get(context.Context, string) (io.ReadCloser, error) { return nil, b.err }
func (b erroringBucket) IsObjNotFoundErr(error) bool                        { return false }

// pathErroringBucket fails Get for one specific object and delegates the rest, so a refresh fails exactly
// one window.
type pathErroringBucket struct {
	objstore.Bucket
	failPath string
	err      error
}

func (b pathErroringBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	if name == b.failPath {
		return nil, b.err
	}
	return b.Bucket.Get(ctx, name)
}

// toggleBucket fails every Get while fail is set, else delegates. It drives transient-failure recovery.
type toggleBucket struct {
	objstore.Bucket
	fail *atomic.Bool
	err  error
}

func (b toggleBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	if b.fail.Load() {
		return nil, b.err
	}
	return b.Bucket.Get(ctx, name)
}

func (b toggleBucket) IsObjNotFoundErr(err error) bool { return b.Bucket.IsObjNotFoundErr(err) }

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

func TestCachedToC_RoundTrip(t *testing.T) {
	window := map[string][]IndexEntry{
		"tenant-a": {
			{Path: "obj-1", Start: time.Unix(100, 0).UTC(), End: time.Unix(200, 0).UTC()},
			{Path: "obj-2", Start: time.Unix(150, 0).UTC(), End: time.Unix(250, 0).UTC()},
		},
		"tenant-b": {{Path: "obj-3", Start: time.Unix(0, 0).UTC(), End: time.Unix(50, 0).UTC()}},
	}
	b, err := encodeCachedToC(window)
	require.NoError(t, err)
	got, err := decodeCachedToC(b)
	require.NoError(t, err)

	require.Len(t, got, 2)
	for tenant, want := range window {
		require.Len(t, got[tenant], len(want))
		for i := range want {
			require.Equal(t, want[i].Path, got[tenant][i].Path)
			require.True(t, want[i].Start.Equal(got[tenant][i].Start))
			require.True(t, want[i].End.Equal(got[tenant][i].End))
		}
	}
}

func TestWarmResolver_GetIndexes_WarmAndFallback(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "tenant-a")
	r := newWarmResolver(t, objstore.NewInMemBucket(), newMapCache())

	// A window warmed with two entries at disjoint times; a query overlaps only the first.
	snap := tocSnapshot{
		"warm-window": {
			"tenant-a": {
				{Path: "obj-early", Start: time.Unix(100, 0).UTC(), End: time.Unix(200, 0).UTC()},
				{Path: "obj-late", Start: time.Unix(1000, 0).UTC(), End: time.Unix(1100, 0).UTC()},
			},
		},
	}
	r.snapshot.Store(&snap)

	t.Run("warm hit, time-filtered", func(t *testing.T) {
		got, err := r.GetIndexes(ctx, []string{"warm-window"}, time.Unix(150, 0).UTC(), time.Unix(160, 0).UTC())
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "obj-early", got[0].Path)
	})

	t.Run("cold window falls back to the lazy resolver", func(t *testing.T) {
		// A fresh resolver so the counters start at zero; it reuses the same warmed snapshot.
		r := newWarmResolver(t, objstore.NewInMemBucket(), newMapCache())
		r.snapshot.Store(&snap)
		// "cold-window" is absent from the snapshot; the lazy resolver reads it from the (empty) bucket,
		// which returns not-found and yields no entries.
		got, err := r.GetIndexes(ctx, []string{"warm-window", "cold-window"}, time.Unix(150, 0).UTC(), time.Unix(160, 0).UTC())
		require.NoError(t, err)
		require.Len(t, got, 1) // only the warm window matched
		require.Equal(t, 1.0, testutil.ToFloat64(r.source.WithLabelValues("cache")))
		require.Equal(t, 1.0, testutil.ToFloat64(r.source.WithLabelValues("storage")))
	})
}

func TestWarmResolver_Refresh_ReadsBucketThenDedupsViaCache(t *testing.T) {
	tenant := tenantID
	ctx := user.InjectOrgID(context.Background(), tenant)

	// Seed one ToC in a recent window so the warmer's [now-48h, now] refresh covers it.
	ts := now.Add(-2 * time.Hour)
	bucket := objstore.NewInMemBucket()
	seedPostingsIndexToC(t, bucket, tenant, ts)

	shared := newMapCache() // shared across instances to exercise cross-instance dedup

	r1 := newWarmResolver(t, bucket, shared)
	r1.refresh(ctx)
	require.Positive(t, testutil.ToFloat64(r1.objectStoreGets), "the first instance reads ToCs from object storage")

	// The seeded window's paths, resolved warm.
	var tablePaths []string
	for p := range IterTableOfContentsPaths(ts.Add(-time.Hour), ts.Add(time.Hour)) {
		tablePaths = append(tablePaths, p)
	}
	warm, err := r1.GetIndexes(ctx, tablePaths, ts.Add(-time.Hour), ts.Add(time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, warm, "the seeded index object resolves from the warm snapshot")

	// Warm output matches the lazy resolver's for the same window.
	lazy := NewTableOfContentsLazyResolver(bucket, log.NewNopLogger())
	lazyOut, err := lazy.GetIndexes(ctx, tablePaths, ts.Add(-time.Hour), ts.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, lazyOut, warm)

	// A second instance sharing the cache warms entirely from memcached — no object-storage reads.
	r2 := newWarmResolver(t, bucket, shared)
	r2.refresh(ctx)
	require.Zero(t, testutil.ToFloat64(r2.objectStoreGets), "the second instance is served from the shared cache")
	require.Positive(t, testutil.ToFloat64(r2.cacheHits))

	// The cache-decoded snapshot resolves identically to the lazy resolver, not just instant-equal.
	warm2, err := r2.GetIndexes(ctx, tablePaths, ts.Add(-time.Hour), ts.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, lazyOut, warm2)
}

func TestWarmResolver_GetIndexes_TenantIsolation(t *testing.T) {
	r := newWarmResolver(t, objstore.NewInMemBucket(), newMapCache())
	snap := tocSnapshot{
		"w": {
			"tenant-a": {{Path: "a-obj", Start: time.Unix(100, 0), End: time.Unix(200, 0)}},
			"tenant-b": {{Path: "b-obj", Start: time.Unix(100, 0), End: time.Unix(200, 0)}},
		},
	}
	r.snapshot.Store(&snap)

	ctxA := user.InjectOrgID(context.Background(), "tenant-a")
	gotA, err := r.GetIndexes(ctxA, []string{"w"}, time.Unix(100, 0), time.Unix(200, 0))
	require.NoError(t, err)
	require.Len(t, gotA, 1)
	require.Equal(t, "a-obj", gotA[0].Path)

	ctxB := user.InjectOrgID(context.Background(), "tenant-b")
	gotB, err := r.GetIndexes(ctxB, []string{"w"}, time.Unix(100, 0), time.Unix(200, 0))
	require.NoError(t, err)
	require.Len(t, gotB, 1)
	require.Equal(t, "b-obj", gotB[0].Path)
}

func TestWarmResolver_GetIndexes_InclusiveBoundaries(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "tenant-a")
	r := newWarmResolver(t, objstore.NewInMemBucket(), newMapCache())
	snap := tocSnapshot{"w": {"tenant-a": {{Path: "obj", Start: time.Unix(100, 0), End: time.Unix(200, 0)}}}}
	r.snapshot.Store(&snap)

	cases := []struct {
		name       string
		start, end time.Time
		want       int
	}{
		{"query end equals object start", time.Unix(0, 0), time.Unix(100, 0), 1},
		{"query start equals object end", time.Unix(200, 0), time.Unix(300, 0), 1},
		{"query ends just before object start", time.Unix(0, 0), time.Unix(99, 0), 0},
		{"query starts just after object end", time.Unix(201, 0), time.Unix(300, 0), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.GetIndexes(ctx, []string{"w"}, tc.start, tc.end)
			require.NoError(t, err)
			require.Len(t, got, tc.want)
		})
	}
}

func TestWarmResolver_Refresh_MultiTenantNoBleed(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	// Pin both timestamps mid-window so their [ts-1h, ts+1h] ranges land in one 12h window regardless of
	// the wall clock. Windows align to 00:00/12:00 UTC (Truncate), so a pair derived from now alone can
	// straddle a boundary and split into two ToCs.
	windowStart := now.Add(-24 * time.Hour).Truncate(MetastoreWindowSize)
	tsA := windowStart.Add(4 * time.Hour)
	tsB := windowStart.Add(5 * time.Hour)
	seedPostingsIndexToC(t, bucket, "tenant-a", tsA)
	seedPostingsIndexToC(t, bucket, "tenant-b", tsB)

	r := newWarmResolver(t, bucket, newMapCache())
	r.refresh(context.Background())

	snap := r.snapshot.Load()
	var win map[string][]IndexEntry
	for _, w := range *snap {
		if _, ok := w["tenant-a"]; ok {
			win = w
			break
		}
	}
	require.NotNil(t, win, "the seeded window is warm")
	require.Len(t, win["tenant-a"], 1)
	require.Len(t, win["tenant-b"], 1)

	a, b := win["tenant-a"][0], win["tenant-b"][0]
	require.NotEmpty(t, a.Path)
	require.NotEmpty(t, b.Path)
	require.NotEqual(t, a.Path, b.Path, "each tenant keeps its own object path")
	require.True(t, a.Start.Equal(tsA.Add(-time.Hour)), "tenant-a start is not bled from tenant-b")
	require.True(t, b.Start.Equal(tsB.Add(-time.Hour)), "tenant-b start is not bled from tenant-a")
}

func TestWarmResolver_Refresh_DropsWindowOnErrorForLazyFallback(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)

	// A window that was warm in the previous snapshot.
	ts := now.Add(-2 * time.Hour)
	var seededPath string
	for p := range IterTableOfContentsPaths(ts, ts) {
		seededPath = p
	}
	require.NotEmpty(t, seededPath)

	// The bucket now fails every read, so the refresh cannot reload the window.
	r := newWarmResolver(t, erroringBucket{err: errors.New("boom")}, newMapCache())
	prev := tocSnapshot{seededPath: {tenantID: {{Path: "obj-old", Start: ts.Add(-time.Hour), End: ts.Add(time.Hour)}}}}
	r.snapshot.Store(&prev)

	r.refresh(ctx)

	require.Positive(t, testutil.ToFloat64(r.refreshErrors))
	snap := r.snapshot.Load()
	require.NotContains(t, *snap, seededPath, "a window that failed to reload is dropped, not served stale")

	// GetIndexes now routes the dropped window to the lazy resolver, which surfaces the bucket failure
	// instead of returning stale warm data.
	_, err := r.GetIndexes(ctx, []string{seededPath}, ts.Add(-time.Hour), ts.Add(time.Hour))
	require.Error(t, err)
}

func TestWarmResolver_Refresh_UndecodableCacheFallsBackToBucket(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	ts := now.Add(-2 * time.Hour)
	bucket := objstore.NewInMemBucket()
	seedPostingsIndexToC(t, bucket, tenantID, ts)

	// The cache returns bytes that are not a valid CachedToC, so every window read decodes-fails and falls
	// through to the bucket. 0x0a declares a length-delimited field; 0x05 promises 5 more bytes that are absent.
	r := newTableOfContentsWarmResolver(fixedCache{value: []byte{0x0a, 0x05}}, bucket, 48*time.Hour, time.Minute, nil, log.NewNopLogger())
	r.refresh(ctx)

	require.Positive(t, testutil.ToFloat64(r.cacheErrors.WithLabelValues("decode")))
	require.Positive(t, testutil.ToFloat64(r.objectStoreGets), "an undecodable cache forces a bucket read")

	var tablePaths []string
	for p := range IterTableOfContentsPaths(ts.Add(-time.Hour), ts.Add(time.Hour)) {
		tablePaths = append(tablePaths, p)
	}
	got, err := r.GetIndexes(ctx, tablePaths, ts.Add(-time.Hour), ts.Add(time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, got, "the window resolves warm from the bucket-read snapshot")
}

func TestWarmResolver_ServiceLifecycle(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	bucket := objstore.NewInMemBucket()
	seedPostingsIndexToC(t, bucket, tenantID, midWindowTS(24*time.Hour))
	spy := &stopSpyCache{cacheStore: newMapCache()}
	r := newTableOfContentsWarmResolver(spy, bucket, 48*time.Hour, time.Minute, nil, log.NewNopLogger())

	require.NoError(t, services.StartAndAwaitRunning(ctx, r))
	require.Eventually(t, func() bool { return r.snapshot.Load() != nil }, time.Second, 10*time.Millisecond,
		"the warm-on-start refresh publishes a snapshot")
	require.NoError(t, services.StopAndAwaitTerminated(ctx, r))
	require.True(t, spy.stopped.Load(), "stopping the service stops the cache")
}

// midWindowTS returns a timestamp about ago in the past, pinned to the middle of its 12h window (windows
// align to 00:00/12:00 UTC), so [ts-1h, ts+1h] never straddles a window boundary regardless of the clock.
func midWindowTS(ago time.Duration) time.Time {
	return now.Add(-ago).Truncate(MetastoreWindowSize).Add(6 * time.Hour)
}

func TestWarmResolver_Refresh_DropsOnlyFailingWindow(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	healthyTS := midWindowTS(24 * time.Hour)
	failingTS := midWindowTS(36 * time.Hour) // a distinct, earlier 12h window, still within the 48h warm window
	bucket := objstore.NewInMemBucket()
	seedPostingsIndexToC(t, bucket, tenantID, healthyTS)
	seedPostingsIndexToC(t, bucket, tenantID, failingTS)

	var healthyPath, failingPath string
	for p := range IterTableOfContentsPaths(healthyTS, healthyTS) {
		healthyPath = p
	}
	for p := range IterTableOfContentsPaths(failingTS, failingTS) {
		failingPath = p
	}
	require.NotEqual(t, healthyPath, failingPath)

	r := newWarmResolver(t, pathErroringBucket{Bucket: bucket, failPath: failingPath, err: errors.New("boom")}, newMapCache())
	r.refresh(ctx)

	require.Equal(t, 1.0, testutil.ToFloat64(r.refreshErrors), "only the one failing window errors")
	snap := r.snapshot.Load()
	require.NotContains(t, *snap, failingPath, "the failing window is dropped")
	require.Contains(t, *snap, healthyPath, "the healthy window is still warmed")
	require.Len(t, (*snap)[healthyPath][tenantID], 1)
}

func TestWarmResolver_Refresh_RecoversWindowAfterTransientError(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	ts := midWindowTS(24 * time.Hour)
	bucket := objstore.NewInMemBucket()
	seedPostingsIndexToC(t, bucket, tenantID, ts)

	var path string
	for p := range IterTableOfContentsPaths(ts, ts) {
		path = p
	}

	fail := &atomic.Bool{}
	fail.Store(true)
	r := newWarmResolver(t, toggleBucket{Bucket: bucket, fail: fail, err: errors.New("boom")}, newMapCache())

	r.refresh(ctx) // the bucket errors, so the window is dropped
	require.NotContains(t, *r.snapshot.Load(), path)

	fail.Store(false)
	r.refresh(ctx) // the bucket recovers, so a dropped window is re-warmed
	snap := r.snapshot.Load()
	require.Contains(t, *snap, path, "a dropped window is re-warmed by a later successful refresh")
	require.Len(t, (*snap)[path][tenantID], 1)
}

func TestWarmResolver_ConcurrentGetIndexesAndRefresh(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	ts := midWindowTS(24 * time.Hour)
	bucket := objstore.NewInMemBucket()
	seedPostingsIndexToC(t, bucket, tenantID, ts)
	r := newWarmResolver(t, bucket, newMapCache())
	r.refresh(ctx) // publish an initial snapshot

	var tablePaths []string
	for p := range IterTableOfContentsPaths(ts.Add(-time.Hour), ts.Add(time.Hour)) {
		tablePaths = append(tablePaths, p)
	}

	// Readers query while a refresher republishes; -race guards the atomic-snapshot no-mutation contract.
	var readErr atomic.Value
	stop := make(chan struct{})
	var refresher sync.WaitGroup
	refresher.Add(1)
	go func() {
		defer refresher.Done()
		for {
			select {
			case <-stop:
				return
			default:
				r.refresh(ctx)
			}
		}
	}()

	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range 200 {
				got, err := r.GetIndexes(ctx, tablePaths, ts.Add(-time.Hour), ts.Add(time.Hour))
				if err != nil {
					readErr.Store(err)
					return
				}
				if len(got) == 0 {
					readErr.Store(errors.New("warm read returned no entries"))
					return
				}
			}
		}()
	}
	readers.Wait()
	close(stop)
	refresher.Wait()

	if v := readErr.Load(); v != nil {
		t.Fatalf("concurrent GetIndexes failed: %v", v.(error))
	}
}

func TestWarmResolver_Refresh_DoesNotWarmBeyondWarmWindow(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	oldTS := midWindowTS(96 * time.Hour) // well outside the 48h warm window
	bucket := objstore.NewInMemBucket()
	seedPostingsIndexToC(t, bucket, tenantID, oldTS)
	r := newWarmResolver(t, bucket, newMapCache())
	r.refresh(ctx)

	var oldPath string
	for p := range IterTableOfContentsPaths(oldTS, oldTS) {
		oldPath = p
	}
	require.NotContains(t, *r.snapshot.Load(), oldPath, "a ToC older than the warm window is not warmed")

	// The lazy fallback still resolves the out-of-window ToC, counted as a storage read.
	got, err := r.GetIndexes(ctx, []string{oldPath}, oldTS.Add(-time.Hour), oldTS.Add(time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, got)
	require.Positive(t, testutil.ToFloat64(r.source.WithLabelValues("storage")))
}

func TestWarmResolver_GetIndexes_ServesKnownEmptyWindowWithoutLazyRead(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	bucket := objstore.NewInMemBucket() // no ToCs: every window warms as known-empty
	r := newWarmResolver(t, bucket, newMapCache())
	r.refresh(ctx)

	queryTS := now.Add(-13 * time.Hour) // inside the warm window
	var path string
	for p := range IterTableOfContentsPaths(queryTS, queryTS) {
		path = p
	}
	require.Contains(t, *r.snapshot.Load(), path, "an empty window is warmed as known-empty, not skipped")

	got, err := r.GetIndexes(ctx, []string{path}, queryTS, queryTS)
	require.NoError(t, err)
	require.Empty(t, got)
	require.Equal(t, 1.0, testutil.ToFloat64(r.source.WithLabelValues("cache")), "served from the snapshot")
	require.Zero(t, testutil.ToFloat64(r.source.WithLabelValues("storage")), "not a lazy read")
}

func TestWarmResolver_Refresh_CacheErrorsDegradeToBucket(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	ts := midWindowTS(24 * time.Hour)
	bucket := objstore.NewInMemBucket()
	seedPostingsIndexToC(t, bucket, tenantID, ts)
	r := newTableOfContentsWarmResolver(erroringCache{}, bucket, 48*time.Hour, time.Minute, nil, log.NewNopLogger())
	r.refresh(ctx)

	require.Positive(t, testutil.ToFloat64(r.cacheErrors.WithLabelValues("fetch")))
	require.Positive(t, testutil.ToFloat64(r.cacheErrors.WithLabelValues("store")))
	require.Positive(t, testutil.ToFloat64(r.objectStoreGets), "cache errors degrade to a bucket read")
	require.Zero(t, testutil.ToFloat64(r.cacheMisses), "a fetch error is counted as an error, not a miss")

	var path string
	for p := range IterTableOfContentsPaths(ts, ts) {
		path = p
	}
	require.Contains(t, *r.snapshot.Load(), path, "the window still warms from the bucket despite the broken cache")
}

func TestWarmResolver_NextRefreshDelay(t *testing.T) {
	r := newTableOfContentsWarmResolver(newMapCache(), objstore.NewInMemBucket(), 48*time.Hour, time.Minute, nil, log.NewNopLogger())
	for range 1000 {
		d := r.nextRefreshDelay()
		require.GreaterOrEqual(t, d, time.Minute, "the delay never drops below the refresh interval")
		require.Less(t, d, time.Minute+time.Minute/10)
	}

	// A sub-10ns interval makes the jitter span round to zero, so the delay is exactly the interval.
	tiny := newTableOfContentsWarmResolver(newMapCache(), objstore.NewInMemBucket(), time.Hour, 5*time.Nanosecond, nil, log.NewNopLogger())
	require.Equal(t, 5*time.Nanosecond, tiny.nextRefreshDelay())
}

func TestWarmResolverConfig_Validate(t *testing.T) {
	require.NoError(t, (&TableOfContentsWarmResolverConfig{WarmWindow: 48 * time.Hour, RefreshInterval: time.Minute}).Validate())
	require.Error(t, (&TableOfContentsWarmResolverConfig{WarmWindow: 48 * time.Hour, RefreshInterval: 0}).Validate())
	require.Error(t, (&TableOfContentsWarmResolverConfig{WarmWindow: 0, RefreshInterval: time.Minute}).Validate())
}

func TestNewTableOfContentsWarmResolver_RejectsInvalidConfig(t *testing.T) {
	_, err := NewTableOfContentsWarmResolver(
		TableOfContentsWarmResolverConfig{WarmWindow: 48 * time.Hour, RefreshInterval: 0},
		Config{}, objstore.NewInMemBucket(), nil, log.NewNopLogger())
	require.Error(t, err, "the constructor validates the config so a direct caller cannot build a busy-looping resolver")
}

func TestWarmResolver_DerivedCacheTTLExpiresBeforeNextRefresh(t *testing.T) {
	// The warm cache TTL (RefreshInterval*9/10) must be below the minimum refresh delay (RefreshInterval),
	// so an instance's own cached window always expires before it refreshes again and repopulates it.
	interval := time.Minute
	ttl := interval * 9 / 10
	r := newTableOfContentsWarmResolver(newMapCache(), objstore.NewInMemBucket(), 48*time.Hour, interval, nil, log.NewNopLogger())
	require.Less(t, ttl, r.nextRefreshDelay())
}
