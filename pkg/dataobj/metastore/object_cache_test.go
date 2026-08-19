package metastore

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

func cacheTestReq() SectionsRequest {
	return SectionsRequest{
		Start:    time.Unix(1000, 0).UTC(),
		End:      time.Unix(2000, 0).UTC(),
		Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
	}
}

func cacheTestIndexes() []IndexEntry {
	return []IndexEntry{
		{Path: "a", Start: time.Unix(0, 0).UTC(), End: time.Unix(100, 0).UTC()},
		{Path: "b", Start: time.Unix(100, 0).UTC(), End: time.Unix(200, 0).UTC()},
	}
}

func cacheTestSections() []*DataobjSectionDescriptor {
	return []*DataobjSectionDescriptor{
		{
			SectionKey:          SectionKey{ObjectPath: "obj-a", SectionIdx: 0},
			StreamIDs:           []int64{1, 2, 3},
			RowCount:            42,
			Size:                4096,
			Start:               time.Unix(1100, 0).UTC(),
			End:                 time.Unix(1900, 0).UTC(),
			AmbiguousPredicates: []string{"env"},
		},
		{
			SectionKey: SectionKey{ObjectPath: "obj-b", SectionIdx: 2},
			StreamIDs:  []int64{7},
			Start:      time.Unix(1200, 0).UTC(),
			End:        time.Unix(1800, 0).UTC(),
		},
	}
}

// counterValue returns the summed value of the named counter across all label sets in reg, or 0 if the
// metric was never registered (an unregistered cache exposes nothing).
func counterValue(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		var sum float64
		for _, m := range mf.GetMetric() {
			sum += m.GetCounter().GetValue()
		}
		return sum
	}
	return 0
}

// histogramCount returns the observation count of the named histogram across all label sets in reg.
func histogramCount(t *testing.T, reg *prometheus.Registry, name string) uint64 {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		var n uint64
		for _, m := range mf.GetMetric() {
			n += m.GetHistogram().GetSampleCount()
		}
		return n
	}
	return 0
}

func requireSectionsEqual(t *testing.T, want, got []*DataobjSectionDescriptor) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		require.Equal(t, want[i].ObjectPath, got[i].ObjectPath)
		require.Equal(t, want[i].SectionIdx, got[i].SectionIdx)
		require.Equal(t, want[i].StreamIDs, got[i].StreamIDs)
		require.Equal(t, want[i].RowCount, got[i].RowCount)
		require.Equal(t, want[i].Size, got[i].Size)
		require.True(t, want[i].Start.Equal(got[i].Start), "start %v != %v", want[i].Start, got[i].Start)
		require.True(t, want[i].End.Equal(got[i].End), "end %v != %v", want[i].End, got[i].End)
		require.Equal(t, want[i].AmbiguousPredicates, got[i].AmbiguousPredicates)
	}
}

func TestSectionsCache_GetPut(t *testing.T) {
	req := cacheTestReq()
	indexes := cacheTestIndexes()
	sections := cacheTestSections()

	t.Run("round-trip preserves every field", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		c := NewSectionsCache(newMapCache(), reg, log.NewNopLogger())
		c.Put(context.Background(), "tenant", req, indexes, sections)

		got, hit := c.Get(context.Background(), "tenant", req, indexes)
		require.True(t, hit)
		requireSectionsEqual(t, sections, got)
		require.Equal(t, 1.0, counterValue(t, reg, "loki_metastore_sections_cache_hits_total"))
		require.Equal(t, 0.0, counterValue(t, reg, "loki_metastore_sections_cache_misses_total"))
	})

	t.Run("empty cache is a miss", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		c := NewSectionsCache(newMapCache(), reg, log.NewNopLogger())
		_, hit := c.Get(context.Background(), "tenant", req, indexes)
		require.False(t, hit)
		require.Equal(t, 1.0, counterValue(t, reg, "loki_metastore_sections_cache_misses_total"))
	})

	t.Run("fetch error degrades to a miss", func(t *testing.T) {
		c := NewSectionsCache(erroringCache{}, nil, log.NewNopLogger())
		_, hit := c.Get(context.Background(), "tenant", req, indexes)
		require.False(t, hit)
	})

	t.Run("undecodable entry degrades to a miss", func(t *testing.T) {
		c := NewSectionsCache(fixedCache{value: []byte("not-a-proto")}, nil, log.NewNopLogger())
		_, hit := c.Get(context.Background(), "tenant", req, indexes)
		require.False(t, hit)
	})

	// A stored entry that differs from the request on any keyed input is a hash collision; get must treat
	// it as a miss rather than return the wrong sections. fixedCache forces the entry to be returned under
	// the request's key, so each case exercises one branch of the re-check.
	t.Run("key collision degrades to a miss", func(t *testing.T) {
		matching := CachedSections{
			Matchers:   stableMatchers(req.Matchers),
			Predicates: stableMatchers(req.Predicates),
			StartNanos: req.Start.UnixNano(),
			EndNanos:   req.End.UnixNano(),
			Indexes:    toCachedIndexEntries(stableIndexEntries(indexes)),
			Sections:   toCachedSectionDescriptors(sections),
		}
		for _, tc := range []struct {
			name  string
			mutID func(e *CachedSections)
		}{
			{"matchers differ", func(e *CachedSections) {
				e.Matchers = stableMatchers([]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "other")})
			}},
			{"predicates differ", func(e *CachedSections) {
				e.Predicates = stableMatchers([]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "env", "prod")})
			}},
			{"window differs", func(e *CachedSections) { e.EndNanos = req.End.Add(time.Hour).UnixNano() }},
			{"index set differs in length", func(e *CachedSections) {
				e.Indexes = toCachedIndexEntries(stableIndexEntries(append(append([]IndexEntry(nil), indexes...), IndexEntry{Path: "c"})))
			}},
			{"index set differs in path", func(e *CachedSections) {
				e.Indexes = toCachedIndexEntries(stableIndexEntries([]IndexEntry{indexes[0], {Path: "z", Start: indexes[1].Start, End: indexes[1].End}}))
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				entry := matching
				tc.mutID(&entry)
				b, err := entry.Marshal()
				require.NoError(t, err)

				c := NewSectionsCache(fixedCache{value: b}, nil, log.NewNopLogger())
				_, hit := c.Get(context.Background(), "tenant", req, indexes)
				require.False(t, hit)
			})
		}
	})

	t.Run("predicate mismatch degrades to a miss", func(t *testing.T) {
		c := NewSectionsCache(newMapCache(), nil, log.NewNopLogger())
		c.Put(context.Background(), "tenant", req, indexes, sections)

		// Same matchers/window/indexes but a different predicate: not the same resolution.
		withPredicate := req
		withPredicate.Predicates = []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "env", "prod")}
		_, hit := c.Get(context.Background(), "tenant", withPredicate, indexes)
		require.False(t, hit)
	})
}

func TestSectionsCacheKey(t *testing.T) {
	req := cacheTestReq()
	indexes := cacheTestIndexes()
	base := sectionsCacheKey("tenant", req, indexes)

	t.Run("stable for identical inputs", func(t *testing.T) {
		require.Equal(t, base, sectionsCacheKey("tenant", req, indexes))
	})

	t.Run("has the metastore namespace prefix", func(t *testing.T) {
		require.Contains(t, base, sectionsCacheKeyPrefix+sectionsCacheKeyVersion+":")
	})

	t.Run("changes with tenant", func(t *testing.T) {
		require.NotEqual(t, base, sectionsCacheKey("other", req, indexes))
	})

	t.Run("changes with matchers", func(t *testing.T) {
		other := req
		other.Matchers = []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "bar")}
		require.NotEqual(t, base, sectionsCacheKey("tenant", other, indexes))
	})

	t.Run("changes with predicates", func(t *testing.T) {
		other := req
		other.Predicates = []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "env", "prod")}
		require.NotEqual(t, base, sectionsCacheKey("tenant", other, indexes))
	})

	t.Run("changes with start/end", func(t *testing.T) {
		other := req
		other.End = req.End.Add(time.Hour)
		require.NotEqual(t, base, sectionsCacheKey("tenant", other, indexes))
	})

	t.Run("changes with the index-object set", func(t *testing.T) {
		withNew := append(append([]IndexEntry(nil), indexes...), IndexEntry{Path: "c"})
		require.NotEqual(t, base, sectionsCacheKey("tenant", req, withNew))
	})

	t.Run("is independent of matcher and index order", func(t *testing.T) {
		reordered := req
		reordered.Matchers = []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "z", "1"),
			labels.MustNewMatcher(labels.MatchEqual, "a", "1"),
		}
		req.Matchers = []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "a", "1"),
			labels.MustNewMatcher(labels.MatchEqual, "z", "1"),
		}
		require.Equal(t, sectionsCacheKey("tenant", req, indexes), sectionsCacheKey("tenant", reordered, indexes))

		rev := []IndexEntry{indexes[1], indexes[0]}
		require.Equal(t, sectionsCacheKey("tenant", req, indexes), sectionsCacheKey("tenant", req, rev))
	})
}

func TestObjectMetastore_SectionsCaching(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	start, end := now.Add(-5*time.Hour), now.Add(5*time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	newSeeded := func(t *testing.T) (*ObjectMetastore, *prometheus.Registry) {
		t.Helper()
		bucket := objstore.NewInMemBucket()
		seedPostingsIndexToC(t, bucket, tenantID, now.Add(-time.Hour))
		reg := prometheus.NewRegistry()
		m := NewObjectMetastore(bucket, Config{ReadPostingsSections: true}, log.NewNopLogger(),
			NewObjectMetastoreMetrics(nil), WithSectionsCache(NewSectionsCache(newMapCache(), reg, log.NewNopLogger())))
		return m, reg
	}
	misses := func(reg *prometheus.Registry) float64 {
		return counterValue(t, reg, "loki_metastore_sections_cache_misses_total")
	}
	hits := func(reg *prometheus.Registry) float64 {
		return counterValue(t, reg, "loki_metastore_sections_cache_hits_total")
	}

	t.Run("second identical call is served from cache", func(t *testing.T) {
		m, reg := newSeeded(t)

		resp1, err := m.Sections(ctx, SectionsRequest{Start: start, End: end, Matchers: matchers})
		require.NoError(t, err)
		require.NotEmpty(t, resp1.Sections)
		require.Equal(t, 1.0, misses(reg))
		require.Equal(t, 0.0, hits(reg))

		resp2, err := m.Sections(ctx, SectionsRequest{Start: start, End: end, Matchers: matchers})
		require.NoError(t, err)
		require.Len(t, resp2.Sections, len(resp1.Sections))
		require.Equal(t, 1.0, misses(reg), "the second call must not re-resolve")
		require.Equal(t, 1.0, hits(reg))
	})

	t.Run("different matchers re-resolve", func(t *testing.T) {
		m, reg := newSeeded(t)

		_, err := m.Sections(ctx, SectionsRequest{Start: start, End: end, Matchers: matchers})
		require.NoError(t, err)
		other := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "bar")}
		_, err = m.Sections(ctx, SectionsRequest{Start: start, End: end, Matchers: other})
		require.NoError(t, err)
		require.Equal(t, 2.0, misses(reg))
	})

	t.Run("resolvedSectionsTotalDuration times whole call, skips empty windows", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPostingsIndexToC(t, bucket, tenantID, now.Add(-time.Hour))
		reg := prometheus.NewRegistry()
		m := NewObjectMetastore(bucket, Config{ReadPostingsSections: true}, log.NewNopLogger(),
			NewObjectMetastoreMetrics(reg), WithSectionsCache(NewSectionsCache(newMapCache(), nil, log.NewNopLogger())))
		const dur = "loki_metastore_resolved_sections_total_duration_seconds"

		_, err := m.Sections(ctx, SectionsRequest{Start: start, End: end, Matchers: matchers})
		require.NoError(t, err)
		require.Equal(t, uint64(1), histogramCount(t, reg, dur), "a resolution is timed")

		_, err = m.Sections(ctx, SectionsRequest{Start: start, End: end, Matchers: matchers})
		require.NoError(t, err)
		require.Equal(t, uint64(2), histogramCount(t, reg, dur), "a cache hit is timed too")

		// A window with no index objects returns early, before the timer is observed.
		_, err = m.Sections(ctx, SectionsRequest{Start: now.Add(-100 * 24 * time.Hour), End: now.Add(-99 * 24 * time.Hour), Matchers: matchers})
		require.NoError(t, err)
		require.Equal(t, uint64(2), histogramCount(t, reg, dur), "an empty window is not timed")
	})
}

// TestObjectMetastore_SectionsSingleFlight covers the singleflight that collapses concurrent identical
// Sections resolutions. A blocking cache parks the leader inside Get, holding the flight, so the tests
// can observe that followers collapse onto it rather than resolving independently.
func TestObjectMetastore_SectionsSingleFlight(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	req := SectionsRequest{
		Start:    now.Add(-5 * time.Hour),
		End:      now.Add(5 * time.Hour),
		Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
	}

	newBlocked := func(t *testing.T) (*ObjectMetastore, *blockingSectionsCache) {
		t.Helper()
		bucket := objstore.NewInMemBucket()
		seedPostingsIndexToC(t, bucket, tenantID, now.Add(-time.Hour))
		bc := newBlockingSectionsCache()
		m := NewObjectMetastore(bucket, Config{ReadPostingsSections: true}, log.NewNopLogger(),
			NewObjectMetastoreMetrics(nil), WithSectionsCache(bc))
		return m, bc
	}

	t.Run("collapses concurrent calls onto one leader", func(t *testing.T) {
		m, bc := newBlocked(t)

		const n = 8
		var wg sync.WaitGroup
		wg.Add(n)
		errs := make([]error, n)
		resps := make([]SectionsResponse, n)
		for i := range n {
			go func(i int) { defer wg.Done(); resps[i], errs[i] = m.Sections(ctx, req) }(i)
		}

		<-bc.entered
		// The parked leader holds the flight, so it can neither return nor populate the cache. A follower
		// that started its own resolution would enter Get a second time; it never does. The window also
		// gives followers time to reach the singleflight and collapse.
		require.Never(t, func() bool { return bc.gets.Load() != 1 }, 200*time.Millisecond, 10*time.Millisecond)

		close(bc.release)
		wg.Wait()
		for i := range n {
			require.NoError(t, errs[i])
			require.NotEmpty(t, resps[i].Sections, "every caller receives the leader's resolved sections")
		}
	})

	t.Run("leader cancellation does not fail waiters", func(t *testing.T) {
		m, bc := newBlocked(t)

		leaderCtx, cancelLeader := context.WithCancel(ctx)
		go func() { _, _ = m.Sections(leaderCtx, req) }()
		<-bc.entered

		waiterErr := make(chan error, 1)
		go func() { _, err := m.Sections(ctx, req); waiterErr <- err }()
		require.Never(t, func() bool { return bc.gets.Load() != 1 }, 200*time.Millisecond, 10*time.Millisecond)

		cancelLeader()    // the leader disconnects mid-flight
		close(bc.release) // let the detached resolution finish

		// The shared work runs on a context detached from the leader, so the waiter on a live context
		// still gets a result instead of the leader's cancellation.
		require.NoError(t, <-waiterErr)
	})

	t.Run("resolution times out when the flight is stuck", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPostingsIndexToC(t, bucket, tenantID, now.Add(-time.Hour))
		// The wrapper blocks the index-object read (not the ToC read GetIndexes needs) until the detached
		// resolve deadline fires, so Sections returns the deadline error instead of hanging.
		m := NewObjectMetastore(ctxBlockingBucket{Bucket: bucket}, Config{ReadPostingsSections: true}, log.NewNopLogger(),
			NewObjectMetastoreMetrics(nil), WithSectionsResolveTimeout(50*time.Millisecond))

		_, err := m.Sections(ctx, req)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

// seedPostingsIndexToC uploads a postings index object holding one {app="foo"} label posting at ts and
// registers it in the table of contents, so GetIndexes lists it and Sections resolves it for real.
func seedPostingsIndexToC(t *testing.T, bucket objstore.Bucket, tenant string, ts time.Time) {
	t.Helper()
	ctx := user.InjectOrgID(context.Background(), tenant)

	obj, closer := buildPostingsIndexObject(t, tenant, []postings.LabelObservation{
		{ObjectPath: "src-obj", SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: 1, Timestamp: ts},
	})
	defer closer()

	up := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
	path, err := up.Upload(ctx, obj)
	require.NoError(t, err)

	tr := []multitenancy.TimeRange{{Tenant: tenant, MinTime: ts.Add(-time.Hour), MaxTime: ts.Add(time.Hour)}}
	require.NoError(t, NewTableOfContentsWriter(bucket, log.NewNopLogger()).WriteEntry(ctx, path, tr))
}

// ctxBlockingBucket blocks reads of non-ToC objects until the context is done, returning its error. ToC
// reads (GetIndexes) pass through, so a resolution stalls on the index-object read and hits the resolve
// timeout deterministically even though the in-memory bucket itself ignores the context.
type ctxBlockingBucket struct {
	objstore.Bucket
}

func (b ctxBlockingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	if strings.HasPrefix(name, TocPrefix) {
		return b.Bucket.Get(ctx, name)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (b ctxBlockingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	if strings.HasPrefix(name, TocPrefix) {
		return b.Bucket.GetRange(ctx, name, off, length)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (b ctxBlockingBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	if strings.HasPrefix(name, TocPrefix) {
		return b.Bucket.Attributes(ctx, name)
	}
	<-ctx.Done()
	return objstore.ObjectAttributes{}, ctx.Err()
}

// blockingSectionsCache is a SectionsCache whose Get parks the singleflight leader until release is
// closed, so tests can hold a resolution in flight. It always misses. gets counts Get entries; with the
// singleflight, only the leader enters Get.
type blockingSectionsCache struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	gets    atomic.Int32
}

func newBlockingSectionsCache() *blockingSectionsCache {
	return &blockingSectionsCache{entered: make(chan struct{}), release: make(chan struct{})}
}

func (c *blockingSectionsCache) Get(ctx context.Context, _ string, _ SectionsRequest, _ []IndexEntry) ([]*DataobjSectionDescriptor, bool) {
	c.gets.Add(1)
	c.once.Do(func() { close(c.entered) })
	select {
	case <-c.release:
	case <-ctx.Done():
	}
	return nil, false
}

func (c *blockingSectionsCache) Put(context.Context, string, SectionsRequest, []IndexEntry, []*DataobjSectionDescriptor) {
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

func (c *mapCache) Stop()                         {}
func (c *mapCache) GetCacheType() stats.CacheType { return "test" }

// fixedCache always returns value for any single-key fetch.
type fixedCache struct{ value []byte }

func (fixedCache) Store(context.Context, []string, [][]byte) error { return nil }
func (c fixedCache) Fetch(_ context.Context, keys []string) ([]string, [][]byte, []string, error) {
	return keys, [][]byte{c.value}, nil, nil
}
func (fixedCache) Stop()                         {}
func (fixedCache) GetCacheType() stats.CacheType { return "test" }

// erroringCache fails every operation.
type erroringCache struct{}

func (erroringCache) Store(context.Context, []string, [][]byte) error {
	return errors.New("store boom")
}
func (erroringCache) Fetch(context.Context, []string) ([]string, [][]byte, []string, error) {
	return nil, nil, nil, errors.New("fetch boom")
}
func (erroringCache) Stop()                         {}
func (erroringCache) GetCacheType() stats.CacheType { return "test" }
