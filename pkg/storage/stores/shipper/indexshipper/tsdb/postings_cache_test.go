package tsdb

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestPostingsCacheCanonicalKey(t *testing.T) {
	m1 := labels.MustNewMatcher(labels.MatchEqual, "app", "api")
	m2 := labels.MustNewMatcher(labels.MatchRegexp, "cluster", "prod|staging")
	key1 := postingsKey(objectIdentity("prefix", "table", "tenant", "file.tsdb"), []*labels.Matcher{m1, m2})
	key2 := postingsKey(objectIdentity("prefix", "table", "tenant", "file.tsdb"), []*labels.Matcher{m2, m1})
	require.Equal(t, key1, key2)
	require.NotEqual(t,
		postingsKey("id", []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "~api")}),
		postingsKey("id", []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "app", "api")}),
	)
	require.NotEqual(t,
		objectIdentity("prefix", "table", "tenant", "file.tsdb"),
		objectIdentity("prefix", "table", "other", "file.tsdb"),
	)
	require.NotEqual(t,
		postingsKey("id", []*labels.Matcher{m1}),
		postingsKey("other-id", []*labels.Matcher{m1}),
	)
}

func TestCommonMultiTenantPostingsCache(t *testing.T) {
	root := t.TempDir()
	path := setupMultiTenantIndex(t, index.FormatV3, map[string][]stream{
		"tenant-a": {{labels: labels.FromStrings("app", "api"), fp: 1, chunks: index.ChunkMetas{{MinTime: 0, MaxTime: 10, Checksum: 11}}}},
		"tenant-b": {{labels: labels.FromStrings("app", "api"), fp: 2, chunks: index.ChunkMetas{{MinTime: 0, MaxTime: 10, Checksum: 22}}}},
	}, filepath.Join(root, "table"), time.Unix(1, 0))
	c := newTestPostingsCache(t, cache.EmbeddedCacheConfig{MaxSizeItems: 10})
	file, err := openShippableTSDBWithPostingsCache(path, index.MmapOptions{}, c, "prefix", root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	idx := NewMultiTenantIndex(file.(*TSDBFile))
	matcher := labels.MustNewMatcher(labels.MatchEqual, "app", "api")
	first, err := idx.GetChunkRefs(context.Background(), "tenant-a", 0, 10, nil, nil, matcher)
	require.NoError(t, err)
	waitForPostingsStore(c)
	second, err := idx.GetChunkRefs(context.Background(), "tenant-a", 0, 10, nil, nil, matcher)
	require.NoError(t, err)

	require.Equal(t, first, second)
	require.Equal(t, float64(2), testutil.ToFloat64(c.fetchedKeys))
	require.Equal(t, float64(1), testutil.ToFloat64(c.hits))

	_, err = idx.GetChunkRefs(context.Background(), "tenant-b", 0, 10, nil, nil, matcher)
	require.NoError(t, err)
	waitForPostingsStore(c)
}

type postingsReaderSpy struct {
	IndexReader
	calls int
}

func (r *postingsReaderSpy) Postings(name string, filter index.FingerprintFilter, values ...string) (index.Postings, error) {
	r.calls++
	return r.IndexReader.Postings(name, filter, values...)
}

func newPostingsReaderSpy(t *testing.T) *postingsReaderSpy {
	t.Helper()
	var streams []stream
	// Include several fingerprint samples (one per 1024 series) so shard ranges differ.
	for n := 0; n < 4096; n++ {
		streams = append(streams, stream{
			labels: labels.FromStrings("app", "api", "id", fmt.Sprint(n)),
			fp:     model.Fingerprint(uint64(n) * (math.MaxUint64 / 4096)),
			chunks: index.ChunkMetas{{MinTime: 0, MaxTime: 10}},
		})
	}
	path := setupMultiTenantIndex(t, index.FormatV3, map[string][]stream{"tenant": streams}, t.TempDir(), time.Unix(1, 0))
	reader, err := (index.MmapOptions{}).OpenReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	return &postingsReaderSpy{IndexReader: reader}
}

func TestTSDBIndexPostingsCacheSharesShards(t *testing.T) {
	reader := newPostingsReaderSpy(t)
	postingsCache := newTestPostingsCache(t, cache.EmbeddedCacheConfig{MaxSizeItems: 10})
	idx := &TSDBIndex{reader: reader, postingsCache: postingsCache, postingsID: "file"}
	m := labels.MustNewMatcher(labels.MatchEqual, "app", "api")
	low := index.NewShard(0, 2)
	high := index.NewShard(1, 2)
	query := func(filter index.FingerprintFilter, from, through model.Time) []storage.SeriesRef {
		var refs []storage.SeriesRef
		err := idx.forPostings(context.Background(), filter, from, through, []*labels.Matcher{m}, func(p index.Postings) error {
			var err error
			refs, err = index.ExpandPostings(p)
			return err
		})
		require.NoError(t, err)
		return refs
	}

	expected := func(filter index.FingerprintFilter) []storage.SeriesRef {
		p, err := PostingsForMatchers(reader.IndexReader, filter, m)
		require.NoError(t, err)
		refs, err := index.ExpandPostings(p)
		require.NoError(t, err)
		return refs
	}
	lowRefs, highRefs, allRefs := expected(low), expected(high), expected(nil)
	require.NotEmpty(t, lowRefs)
	require.NotEmpty(t, highRefs)
	require.NotEqual(t, lowRefs, highRefs)

	require.Equal(t, lowRefs, query(low, 0, 10))
	waitForPostingsStore(postingsCache)
	require.Equal(t, lowRefs, query(low, 100, 200))
	require.Equal(t, highRefs, query(high, 0, 10))
	require.Equal(t, highRefs, query(high, 100, 200))
	require.Equal(t, allRefs, query(nil, 0, 10))
	require.Equal(t, 1, reader.calls, "all shards and time ranges must reuse one cached postings list")
}

func TestTSDBIndexPostingsCacheDisabled(t *testing.T) {
	reader := newPostingsReaderSpy(t)
	idx := &TSDBIndex{reader: reader}
	m := labels.MustNewMatcher(labels.MatchEqual, "app", "api")
	filter := index.NewShard(1, 2)
	for range 2 {
		err := idx.forPostings(context.Background(), filter, 0, 10, []*labels.Matcher{m}, func(_ index.Postings) error {
			return nil
		})
		require.NoError(t, err)
	}
	require.Equal(t, 2, reader.calls)
}

func TestCachedPostingsAvoidsRecomputation(t *testing.T) {
	c := newTestPostingsCache(t, cache.EmbeddedCacheConfig{MaxSizeItems: 10})
	called := 0
	compute := func() (index.Postings, error) {
		called++
		return index.NewListPostings([]storage.SeriesRef{4, 8}), nil
	}

	_, err := c.cachedPostings(context.Background(), "key", compute)
	require.NoError(t, err)

	require.Equal(t, 1, called)
	waitForPostingsStore(c)

	_, err = c.cachedPostings(context.Background(), "key", func() (index.Postings, error) {
		called++
		return index.EmptyPostings(), nil
	})
	require.NoError(t, err)

	require.NoError(t, err)
	require.Equal(t, 1, called)
}

// Compare full query results, not just approximate candidate postings, because
// the sampled offset boundaries intentionally include neighboring fingerprints.
func TestPostingsCacheShardsMatchUncachedReaders(t *testing.T) {
	var streams []stream
	for n := 0; n < 1024; n++ {
		ls := labels.FromStrings("app", "api", "id", fmt.Sprint(n), "group", fmt.Sprint(n%3))
		if n%2 == 0 {
			ls = labels.FromStrings("app", "api", "id", fmt.Sprint(n))
		}
		streams = append(streams, stream{labels: ls, fp: model.Fingerprint(uint64(n) * (math.MaxUint64 / 1024)), chunks: index.ChunkMetas{{MinTime: 0, MaxTime: 10, Checksum: uint32(n), KB: 1, Entries: 2}}})
	}
	path := setupMultiTenantIndex(t, index.FormatV3, map[string][]stream{"tenant": streams}, t.TempDir(), time.Unix(1, 0))
	for _, opts := range []index.ReaderOptions{index.MmapOptions{}, index.DefaultStreamOptions()} {
		t.Run(fmt.Sprintf("%T", opts), func(t *testing.T) {
			reader, err := opts.OpenReader(path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, reader.Close()) })
			baseline := NewTSDBIndex(reader)
			candidate := NewTSDBIndex(reader)
			pc := newTestPostingsCache(t, cache.EmbeddedCacheConfig{MaxSizeMB: 1})
			candidate.setPostingsCache(pc, "file")
			for _, selector := range []string{`{app="api"}`, `{group="1"}`, `{group!="1"}`, `{group=~"1|2"}`, `{group!~"1|2"}`, `{group=""}`, `{app="missing"}`} {
				t.Run(selector, func(t *testing.T) {
					matchers, err := syntax.ParseMatchers(selector, false)
					require.NoError(t, err)
					before := testutil.ToFloat64(pc.hits)
					filters := []index.FingerprintFilter{nil}
					for _, count := range []uint32{2, 4, 16} {
						for shard := uint32(0); shard < count; shard++ {
							filters = append(filters, index.NewShard(shard, count))
						}
					}
					for _, filter := range filters {
						ctx := context.Background()
						wantRefs, err := baseline.GetChunkRefs(ctx, "tenant", 0, 10, nil, filter, matchers...)
						require.NoError(t, err)
						gotRefs, err := candidate.GetChunkRefs(ctx, "tenant", 0, 10, nil, filter, matchers...)
						require.NoError(t, err)
						require.Equal(t, wantRefs, gotRefs)
						waitForPostingsStore(pc)
						wantSeries, err := baseline.Series(ctx, "tenant", 0, 10, nil, filter, matchers...)
						require.NoError(t, err)
						gotSeries, err := candidate.Series(ctx, "tenant", 0, 10, nil, filter, matchers...)
						require.NoError(t, err)
						require.Equal(t, wantSeries, gotSeries)
						var wantStats, gotStats logproto.IndexStatsResponse
						require.NoError(t, baseline.Stats(ctx, "tenant", 0, 10, &wantStats, filter, nil, matchers...))
						require.NoError(t, candidate.Stats(ctx, "tenant", 0, 10, &gotStats, filter, nil, matchers...))
						require.Equal(t, wantStats, gotStats)
						wantVolume, gotVolume := seriesvolume.NewAccumulator(2048, 2048), seriesvolume.NewAccumulator(2048, 2048)
						require.NoError(t, baseline.Volume(ctx, "tenant", 0, 10, wantVolume, filter, nil, nil, seriesvolume.Series, matchers...))
						require.NoError(t, candidate.Volume(ctx, "tenant", 0, 10, gotVolume, filter, nil, nil, seriesvolume.Series, matchers...))
						require.Equal(t, wantVolume.Volumes(), gotVolume.Volumes())
					}
					require.Greater(t, testutil.ToFloat64(pc.hits), before)
				})
			}
		})
	}
}

func newTestPostingsCache(t testing.TB, cfg cache.EmbeddedCacheConfig) *postingsCache {
	t.Helper()
	cfg.Enabled = true
	c := newPostingsCache(cfg, "test-embedded-cache", nil, log.NewNopLogger())
	require.NotNil(t, c)
	t.Cleanup(c.Stop)
	return c
}

func waitForPostingsStore(c *postingsCache) {
	c.mu.Lock()
	c.mu.Unlock()
}

func storePostingsAndWait(t testing.TB, c *postingsCache, ctx context.Context, key string, refs []storage.SeriesRef) {
	t.Helper()
	c.store(ctx, key, refs)
	waitForPostingsStore(c)
}

func TestPostingsCacheIteratorsAdvanceIndependently(t *testing.T) {
	c := newTestPostingsCache(t, cache.EmbeddedCacheConfig{MaxSizeItems: 1})
	ctx := context.Background()
	want := []storage.SeriesRef{3, 6, 9}
	storePostingsAndWait(t, c, ctx, "key", want)

	// Two queries hit the same entry but have separate iterator positions.
	first := c.fetchPostings(ctx, "key")
	second := c.fetchPostings(ctx, "key")
	require.NotNil(t, first)
	require.NotNil(t, second)
	require.True(t, first.Seek(6))
	require.Equal(t, storage.SeriesRef(6), first.At())

	// Seeking the first iterator must not skip values in the second.
	all, err := index.ExpandPostings(second)
	require.NoError(t, err)
	require.Equal(t, want, all)

	// Exhausting the second iterator must not exhaust the first.
	require.True(t, first.Next())
	require.Equal(t, storage.SeriesRef(9), first.At())
	require.False(t, first.Next())

	// Neither iterator may change the cached references.
	stored, hit := c.cache.Get(ctx, "key")
	require.True(t, hit)
	require.Equal(t, []storage.SeriesRef{3, 6, 9}, stored)
}

func TestPostingsCacheEmptyResultIsAHit(t *testing.T) {
	c := newTestPostingsCache(t, cache.EmbeddedCacheConfig{MaxSizeItems: 1})
	computations := 0
	compute := func() (index.Postings, error) {
		computations++
		return index.EmptyPostings(), nil
	}
	for range 3 {
		p, err := c.cachedPostings(context.Background(), "no-matching-series", compute)
		require.NoError(t, err)
		require.NotNil(t, p)
		require.False(t, p.Next(), "every query returns an empty iterator")
		require.NoError(t, p.Err())
		waitForPostingsStore(c)
	}
	require.Equal(t, 1, computations, "only the first query should compute the empty result")
	require.Equal(t, float64(2), testutil.ToFloat64(c.hits), "the next two queries should hit the cache")
}

func TestPostingsCacheItemLimitAndRetainedIterator(t *testing.T) {
	c := newTestPostingsCache(t, cache.EmbeddedCacheConfig{MaxSizeItems: 1})
	ctx := context.Background()
	compute := func() (index.Postings, error) { return index.NewListPostings([]storage.SeriesRef{1, 2}), nil }
	p, err := c.cachedPostings(ctx, "first", compute)
	require.NoError(t, err)
	waitForPostingsStore(c)
	_, err = c.cachedPostings(ctx, "second", compute)
	require.NoError(t, err)
	waitForPostingsStore(c)
	_, hit := c.cache.Get(ctx, "first")
	require.False(t, hit, "first should have been evicted due to max size of 1")
	c.Stop()
	refs, err := index.ExpandPostings(p)
	require.NoError(t, err)
	require.Equal(t, []storage.SeriesRef{1, 2}, refs, "eviction/shutdown must not mutate an outstanding iterator")
}

func TestPostingsCacheTTL(t *testing.T) {
	for _, ttl := range []time.Duration{0, time.Second} {
		t.Run(fmt.Sprint(ttl), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := newPostingsCache(cache.EmbeddedCacheConfig{
					Enabled: true, MaxSizeItems: 1, TTL: ttl, PurgeInterval: time.Second,
				}, "ttl", prometheus.NewRegistry(), log.NewNopLogger())
				defer c.Stop()
				_, err := c.cachedPostings(context.Background(), "key", func() (index.Postings, error) {
					return index.EmptyPostings(), nil
				})
				require.NoError(t, err)
				time.Sleep(2 * time.Second)
				synctest.Wait()
				p := c.fetchPostings(context.Background(), "key")
				if ttl == 0 {
					require.NotNil(t, p, "zero TTL disables expiration")
				} else {
					require.Nil(t, p)
				}
			})
		})
	}
}

func TestConfiguredPostingsCacheDisabled(t *testing.T) {
	for _, cfg := range []cache.EmbeddedCacheConfig{
		{},
		{MaxSizeMB: 1},
		{Enabled: true},
	} {
		c := newPostingsCache(cfg, "disabled", prometheus.NewRegistry(), log.NewNopLogger())
		require.Nil(t, c)
	}
}

func TestPostingsCacheCancellation(t *testing.T) {
	c := newTestPostingsCache(t, cache.EmbeddedCacheConfig{MaxSizeItems: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, err := c.cachedPostings(ctx, "key", func() (index.Postings, error) {
		cancel()
		return index.EmptyPostings(), nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, p)
	_, hit := c.cache.Get(context.Background(), "key")
	require.False(t, hit)
}

func BenchmarkPostingsCacheHit(b *testing.B) {
	for _, n := range []int{1024, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			refs := make([]storage.SeriesRef, n)
			for i := range refs {
				refs[i] = storage.SeriesRef(i * 3)
			}
			_, ctx := stats.NewContext(context.Background())
			c := newTestPostingsCache(b, cache.EmbeddedCacheConfig{MaxSizeMB: 16})
			storePostingsAndWait(b, c, ctx, "key", refs)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if c.fetchPostings(ctx, "key") == nil {
					b.Fatal("expected cache hit")
				}
			}
		})
	}
}

func TestPostingsCacheCanceledQueryDoesNotCompute(t *testing.T) {
	c := newTestPostingsCache(t, cache.EmbeddedCacheConfig{MaxSizeItems: 1})
	reader := &postingsReaderSpy{}
	idx := NewTSDBIndex(reader)
	idx.setPostingsCache(c, "file")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := idx.forPostings(ctx, nil, 0, 10, nil, func(index.Postings) error {
		t.Error("canceled query must not invoke the callback")
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, reader.calls)
	require.Zero(t, testutil.ToFloat64(c.fetchedKeys))
}

func TestPostingsCacheFailedComputationIsNotCached(t *testing.T) {
	failure := errors.New("read failed")
	for _, compute := range []func() (index.Postings, error){
		func() (index.Postings, error) { return nil, failure },
		func() (index.Postings, error) { return index.ErrPostings(failure), nil },
	} {
		c := newTestPostingsCache(t, cache.EmbeddedCacheConfig{MaxSizeItems: 2})
		p, err := c.cachedPostings(context.Background(), "key", compute)
		require.ErrorIs(t, err, failure)
		require.Nil(t, p)
		require.Nil(t, c.fetchPostings(context.Background(), "key"))
	}
}
