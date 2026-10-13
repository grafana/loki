package tsdb

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/golang/snappy"
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

type postingsTestCache struct {
	mu       sync.Mutex
	entries  map[string][]byte
	fetchErr error
	storeErr error
	fetches  int
	stores   int
	hits     int
}

func (c *postingsTestCache) Store(_ context.Context, keys []string, bufs [][]byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stores++
	if c.storeErr != nil {
		return c.storeErr
	}
	if c.entries == nil {
		c.entries = map[string][]byte{}
	}
	c.entries[keys[0]] = bufs[0]
	return nil
}

func (c *postingsTestCache) Fetch(_ context.Context, keys []string) ([]string, [][]byte, []string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fetches++
	if c.fetchErr != nil {
		return nil, nil, nil, c.fetchErr
	}
	buf, ok := c.entries[keys[0]]
	if !ok {
		return nil, nil, keys, nil
	}
	c.hits++
	return keys, [][]byte{buf}, nil, nil
}

func (c *postingsTestCache) snapshot() *postingsTestCache {
	c.mu.Lock()
	defer c.mu.Unlock()
	entries := make(map[string][]byte, len(c.entries))
	for key, value := range c.entries {
		entries[key] = value
	}
	return &postingsTestCache{entries: entries, stores: c.stores, fetches: c.fetches, hits: c.hits}
}

func waitForPostingsStores(t *testing.T, c *postingsTestCache, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return c.snapshot().stores == n }, time.Second*5, time.Millisecond)
}

func (*postingsTestCache) Stop() {}

func (*postingsTestCache) GetCacheType() stats.CacheType { return stats.IndexCache }

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
	backend := &postingsTestCache{}
	c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
	t.Cleanup(c.Stop)
	file, err := openShippableTSDBWithPostingsCache(path, index.MmapOptions{}, c, "prefix", root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	idx := NewMultiTenantIndex(file.(*TSDBFile))
	matcher := labels.MustNewMatcher(labels.MatchEqual, "app", "api")
	first, err := idx.GetChunkRefs(context.Background(), "tenant-a", 0, 10, nil, nil, matcher)
	require.NoError(t, err)
	waitForPostingsStores(t, backend, 1)
	second, err := idx.GetChunkRefs(context.Background(), "tenant-a", 0, 10, nil, nil, matcher)
	require.NoError(t, err)

	require.Equal(t, first, second)
	require.Equal(t, 2, backend.snapshot().fetches, "queries must fetch postings through the cache attached by the opener")
	require.Equal(t, 1, backend.snapshot().stores, "only the first query for each tenant should store postings")
	require.Equal(t, 1, backend.snapshot().hits, "the repeated query must hit cached postings")

	_, err = idx.GetChunkRefs(context.Background(), "tenant-b", 0, 10, nil, nil, matcher)
	require.NoError(t, err)
	waitForPostingsStores(t, backend, 2)
	require.Len(t, backend.snapshot().entries, 2, "tenant matchers must produce separate cache entries")
}

func TestCachedPostingsFailuresRecomputeAndEmptyResultsCache(t *testing.T) {
	backend := &postingsTestCache{fetchErr: errors.New("fetch")}
	c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
	t.Cleanup(c.Stop)
	called := 0
	compute := func() (index.Postings, error) {
		called++
		return index.EmptyPostings(), nil
	}
	_, err := c.cachedPostings(context.Background(), "key", compute)
	require.NoError(t, err)
	require.Equal(t, 1, called)
	require.Empty(t, backend.snapshot().entries)
}

func TestPostingsCodec(t *testing.T) {
	for _, tc := range []struct {
		name      string
		refs      []storage.SeriesRef
		encoded   []byte
		decodeKey string
		encodeErr string
		decodeErr string
	}{
		{name: "roundtrip", refs: []storage.SeriesRef{2, 7, 19}},
		{name: "empty roundtrip"},
		{name: "unsorted references", refs: []storage.SeriesRef{2, 1}, encodeErr: "not sorted"},
		{name: "mismatched key", refs: []storage.SeriesRef{2, 7, 19}, decodeKey: "other", decodeErr: "key mismatch"},
		{name: "invalid header", encoded: []byte{1, 2, 3}, decodeErr: "unsupported postings codec version"},
		{name: "missing key length", encoded: []byte("v1"), decodeErr: "invalid postings cache key length"},
		{name: "truncated key length", encoded: []byte("v1\x80"), decodeErr: "invalid postings cache key length"},
		{name: "truncated key", encoded: []byte("v1\x04key"), decodeErr: "invalid postings cache key length"},
		{name: "invalid compressed data", encoded: append([]byte("v1\x03key"), 0xff), decodeErr: "decompress postings"},
		{name: "truncated delta", encoded: append([]byte("v1\x03key"), snappy.Encode(nil, []byte{0x80})...), decodeErr: "invalid postings delta varint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded := tc.encoded
			if encoded == nil {
				var err error
				encoded, err = encodePostings("key", tc.refs)
				if tc.encodeErr != "" {
					require.ErrorContains(t, err, tc.encodeErr)
					return
				}
				require.NoError(t, err)
			}
			key := tc.decodeKey
			if key == "" {
				key = "key"
			}
			decoded, err := decodePostings(key, encoded)
			if tc.decodeErr != "" {
				require.ErrorContains(t, err, tc.decodeErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.refs, decoded)
		})
	}
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
	backend := &postingsTestCache{}
	postingsCache := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
	t.Cleanup(postingsCache.Stop)
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
	waitForPostingsStores(t, backend, 1)
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
	backend := &postingsTestCache{}
	c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
	t.Cleanup(c.Stop)
	called := 0
	compute := func() (index.Postings, error) {
		called++
		return index.NewListPostings([]storage.SeriesRef{4, 8}), nil
	}

	_, err := c.cachedPostings(context.Background(), "key", compute)
	require.NoError(t, err)

	require.Equal(t, 1, called)
	waitForPostingsStores(t, backend, 1)

	_, err = c.cachedPostings(context.Background(), "key", func() (index.Postings, error) {
		called++
		return index.EmptyPostings(), nil
	})
	require.NoError(t, err)

	require.NoError(t, err)
	require.Equal(t, 1, called)
}

var _ cache.Cache = (*postingsTestCache)(nil)

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
			backend := &postingsTestCache{}
			candidate := NewTSDBIndex(reader)
			pc := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
			t.Cleanup(pc.Stop)
			candidate.setPostingsCache(pc, "file")
			for _, selector := range []string{`{app="api"}`, `{group="1"}`, `{group!="1"}`, `{group=~"1|2"}`, `{group!~"1|2"}`, `{group=""}`, `{app="missing"}`} {
				t.Run(selector, func(t *testing.T) {
					matchers, err := syntax.ParseMatchers(selector, false)
					require.NoError(t, err)
					before := backend.snapshot().stores
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
						waitForPostingsStores(t, backend, before+1)
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
					require.Equal(t, before+1, backend.snapshot().stores, "all shards and query operations must share one entry")
				})
			}
		})
	}
}

type blockingPostingsCache struct {
	postingsTestCache
	started chan context.Context
	release chan struct{}
	stopped chan struct{}
}

func (c *blockingPostingsCache) Store(ctx context.Context, keys []string, bufs [][]byte) error {
	c.started <- ctx
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.release:
		return c.postingsTestCache.Store(ctx, keys, bufs)
	}
}

func (c *blockingPostingsCache) Stop() { close(c.stopped) }

func TestPostingsCacheAsyncWrite(t *testing.T) {
	backend := &blockingPostingsCache{started: make(chan context.Context, 1), release: make(chan struct{}), stopped: make(chan struct{})}
	c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
	t.Cleanup(c.Stop)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan index.Postings, 1)
	errCh := make(chan error, 1)
	go func() {
		p, err := c.cachedPostings(ctx, "key", func() (index.Postings, error) {
			return index.NewListPostings([]storage.SeriesRef{8, 2, 4}), nil
		})
		result <- p
		errCh <- err
	}()
	select {
	case p := <-result:
		require.NoError(t, <-errCh)
		refs, err := index.ExpandPostings(p)
		require.NoError(t, err)
		require.Equal(t, []storage.SeriesRef{2, 4, 8}, refs)
	case <-time.After(5 * time.Second):
		t.Fatal("query blocked on cache population")
	}
	var writeCtx context.Context
	select {
	case writeCtx = <-backend.started:
	case <-time.After(5 * time.Second):
		t.Fatal("cache write did not start")
	}
	cancel()
	require.NoError(t, writeCtx.Err(), "accepted population must survive request cancellation")
	_, hasDeadline := writeCtx.Deadline()
	require.True(t, hasDeadline)
	close(backend.release)
	waitForPostingsStores(t, &backend.postingsTestCache, 1)
	refs, err := decodePostings("key", backend.snapshot().entries[cache.HashKey("key")])
	require.NoError(t, err)
	require.Equal(t, []storage.SeriesRef{2, 4, 8}, refs)
}

func TestPostingsCacheShutdownAndFullQueue(t *testing.T) {
	backend := &blockingPostingsCache{started: make(chan context.Context, 1), release: make(chan struct{}), stopped: make(chan struct{})}
	c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
	t.Cleanup(c.Stop)
	compute := func() (index.Postings, error) { return index.EmptyPostings(), nil }
	_, err := c.cachedPostings(context.Background(), "first", compute)
	require.NoError(t, err)
	select {
	case <-backend.started:
	case <-time.After(5 * time.Second):
		t.Fatal("cache write did not start")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < postingsWriteBuffer+1; i++ {
			_, _ = c.cachedPostings(context.Background(), fmt.Sprint(i), compute)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("full queue blocked queries")
	}
	require.Len(t, c.writes, postingsWriteBuffer)
	stopDone := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(c.Stop)
		}
		wg.Wait()
		close(stopDone)
	}()
	select {
	case <-stopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel active cache write")
	}
	select {
	case <-backend.stopped:
	default:
		t.Fatal("Stop returned before stopping the backend")
	}
	require.Empty(t, c.writes)
	require.Zero(t, backend.snapshot().stores)
	_, err = c.cachedPostings(context.Background(), "after-stop", compute)
	require.NoError(t, err)
	require.Empty(t, c.writes)
}

func TestPostingsCacheCancellationBeforeEnqueue(t *testing.T) {
	backend := &postingsTestCache{}
	c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
	t.Cleanup(c.Stop)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, err := c.cachedPostings(ctx, "key", func() (index.Postings, error) {
		cancel()
		return index.EmptyPostings(), nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, p)
	c.Stop()
	require.Zero(t, backend.snapshot().stores)
}

func TestPostingsCacheWriteTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := &blockingPostingsCache{started: make(chan context.Context, 1), release: make(chan struct{}), stopped: make(chan struct{})}
		c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
		defer c.Stop()
		_, err := c.cachedPostings(context.Background(), "key", func() (index.Postings, error) {
			return index.EmptyPostings(), nil
		})
		require.NoError(t, err)
		writeCtx := <-backend.started
		time.Sleep(postingsWriteTimeout)
		synctest.Wait()
		require.ErrorIs(t, writeCtx.Err(), context.DeadlineExceeded)
		require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.storeFailures))
		require.Zero(t, backend.snapshot().stores)
	})
}
