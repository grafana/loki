package tsdb

import (
	"bytes"
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type postingsTestCache struct {
	entries  map[string][]byte
	fetchErr error
	storeErr error
	fetches  int
	stores   int
	hits     int
}

func (c *postingsTestCache) Store(_ context.Context, keys []string, bufs [][]byte) error {
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

func TestCommonMultiTenantPostingsCache(t *testing.T) {
	root := t.TempDir()
	path := setupMultiTenantIndex(t, index.FormatV3, map[string][]stream{
		"tenant-a": {{labels: labels.FromStrings("app", "api"), fp: 1, chunks: index.ChunkMetas{{MinTime: 0, MaxTime: 10, Checksum: 11}}}},
		"tenant-b": {{labels: labels.FromStrings("app", "api"), fp: 2, chunks: index.ChunkMetas{{MinTime: 0, MaxTime: 10, Checksum: 22}}}},
	}, filepath.Join(root, "table"), time.Unix(1, 0))
	backend := &postingsTestCache{}
	c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
	file, err := openShippableTSDBWithPostingsCache(path, index.MmapOptions{}, c, "prefix", root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	idx := NewMultiTenantIndex(file.(*TSDBFile))
	matcher := labels.MustNewMatcher(labels.MatchEqual, "app", "api")
	for i, tenant := range []string{"tenant-a", "tenant-b"} {
		first, err := idx.GetChunkRefs(context.Background(), tenant, 0, 10, nil, nil, matcher)
		require.NoError(t, err)
		require.Len(t, first, 1)
		require.Equal(t, tenant, first[0].UserID)
		require.Equal(t, uint64(i+1), first[0].Fingerprint)
		require.Equal(t, uint32(11*(i+1)), first[0].Checksum)
		second, err := idx.GetChunkRefs(context.Background(), tenant, 0, 10, nil, nil, matcher)
		require.NoError(t, err)
		require.Equal(t, first, second)
		require.Equal(t, 2*(i+1), backend.fetches, "queries must fetch postings through the cache attached by the opener")
		require.Equal(t, i+1, backend.stores, "only the first query for each tenant should store postings")
		require.Equal(t, i+1, backend.hits, "the repeated query must hit cached postings")
	}
	require.Len(t, backend.entries, 2, "tenant matchers must produce separate cache entries")
}

func (*postingsTestCache) Stop() {}

func (*postingsTestCache) GetCacheType() stats.CacheType { return stats.IndexCache }

func TestPostingsCacheCanonicalKeyAndRoundTrip(t *testing.T) {
	m1 := labels.MustNewMatcher(labels.MatchEqual, "app", "api")
	m2 := labels.MustNewMatcher(labels.MatchRegexp, "cluster", "prod|staging")
	key1 := postingsKey(objectIdentity("prefix", "table", "tenant", "file.tsdb"), nil, []*labels.Matcher{m1, m2})
	key2 := postingsKey(objectIdentity("prefix", "table", "tenant", "file.tsdb"), nil, []*labels.Matcher{m2, m1})
	require.Equal(t, key1, key2)
	require.NotEqual(t,
		postingsKey("id", nil, []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "~api")}),
		postingsKey("id", nil, []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "app", "api")}),
	)
	require.NotEqual(t, objectIdentity("prefix", "table", "tenant", "file.tsdb"), objectIdentity("prefix", "table", "other", "file.tsdb"))
	require.NotEqual(t, postingsKey("id", nil, []*labels.Matcher{m1}), postingsKey("id", index.ShardAnnotation{Shard: 0, Of: 2}, []*labels.Matcher{m1}))

	refs := []storage.SeriesRef{2, 7, 19}
	encoded, err := encodePostings(key1, refs)
	require.NoError(t, err)
	decoded, err := decodePostings(key1, encoded)
	require.NoError(t, err)
	require.Equal(t, refs, decoded)
	_, err = encodePostings(key1, []storage.SeriesRef{2, 1})
	require.ErrorContains(t, err, "not sorted")
	_, err = decodePostings(key1+"x", encoded)
	require.ErrorContains(t, err, "key mismatch")
}

func TestCachedPostingsFailuresRecomputeAndEmptyResultsCache(t *testing.T) {
	backend := &postingsTestCache{fetchErr: errors.New("fetch")}
	c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
	called := 0
	compute := func() (index.Postings, error) {
		called++
		return index.EmptyPostings(), nil
	}
	_, err := c.cachedPostings(context.Background(), "key", compute)
	require.NoError(t, err)
	require.Equal(t, 1, called)
	require.Empty(t, backend.entries)
	backend.fetchErr = nil
	p, err := c.cachedPostings(context.Background(), "key", func() (index.Postings, error) {
		called++
		return index.NewListPostings(nil), nil
	})
	require.NoError(t, err)
	refs, err := index.ExpandPostings(p)
	require.NoError(t, err)
	require.Empty(t, refs)
	require.Equal(t, 2, called)
	require.Contains(t, backend.entries, cache.HashKey("key"))
	_, err = c.cachedPostings(context.Background(), "key", compute)
	require.NoError(t, err)
	require.Equal(t, 2, called)

	backend = &postingsTestCache{storeErr: errors.New("store")}
	c = newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
	_, err = c.cachedPostings(context.Background(), "key", compute)
	require.NoError(t, err)
	require.Equal(t, 3, called)
}

func TestDecodePostingsRejectsWrongPayload(t *testing.T) {
	for _, encoded := range [][]byte{
		{1, 2, 3},
		[]byte("v1"),
		[]byte("v1\x80"),
		[]byte("v1\x04key"),
		append([]byte("v1\x03key"), 0xff),
		append([]byte("v1\x03key"), snappy.Encode(nil, []byte{0x80})...),
	} {
		_, err := decodePostings("key", encoded)
		require.Error(t, err)
	}
}

func TestCachedPostingsStoreFailureInstrumentation(t *testing.T) {
	var logs bytes.Buffer
	backend := &postingsTestCache{storeErr: errors.New("store unavailable")}
	c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewLogfmtLogger(&logs))
	compute := func() (index.Postings, error) {
		return index.NewListPostings([]storage.SeriesRef{2, 7}), nil
	}
	p, err := c.cachedPostings(context.Background(), "key", compute)
	require.NoError(t, err)
	refs, err := index.ExpandPostings(p)
	require.NoError(t, err)
	require.Equal(t, []storage.SeriesRef{2, 7}, refs)
	require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.storeFailures))
	require.Contains(t, logs.String(), "failed to store postings in cache")
	require.Contains(t, logs.String(), "store unavailable")
	backend.storeErr = nil
	_, err = c.cachedPostings(context.Background(), "key", compute)
	require.NoError(t, err)
	require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.storeFailures))
}

func TestCachedPostingsDecodeFailureRecomputes(t *testing.T) {
	backend := &postingsTestCache{entries: map[string][]byte{cache.HashKey("key"): []byte("v1")}}
	c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
	require.Zero(t, testutil.ToFloat64(c.metrics.decodeFailures))
	p, err := c.cachedPostings(context.Background(), "key", func() (index.Postings, error) {
		return index.NewListPostings([]storage.SeriesRef{7}), nil
	})
	require.NoError(t, err)
	refs, err := index.ExpandPostings(p)
	require.NoError(t, err)
	require.Equal(t, []storage.SeriesRef{7}, refs)
	require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.decodeFailures))
	decoded, err := decodePostings("key", backend.entries[cache.HashKey("key")])
	require.NoError(t, err)
	require.Equal(t, refs, decoded)
	_, err = c.cachedPostings(context.Background(), "key", func() (index.Postings, error) {
		t.Fatal("expected a cache hit after replacing the invalid payload")
		return nil, nil
	})
	require.NoError(t, err)
	require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.decodeFailures))
}

func TestPostingsObjectIdentityFromDownloadedPath(t *testing.T) {
	root := "/cache"
	identity, ok := postingsObjectIdentity(root, "/cache/table-a/tenant-a/file-a.tsdb", "prefix-a")
	require.True(t, ok)
	require.Equal(t, objectIdentity("prefix-a", "table-a", "tenant-a", "file-a.tsdb"), identity)
	require.NotEqual(t, identity, objectIdentity("prefix-b", "table-a", "tenant-a", "file-a.tsdb"))
	require.NotEqual(t, identity, objectIdentity("prefix-a", "table-b", "tenant-a", "file-a.tsdb"))
	identity, ok = postingsObjectIdentity(root, "/cache/table-a/tenant-a/file-b.tsdb", "prefix-a")
	require.True(t, ok)
	require.NotEqual(t, identity, objectIdentity("prefix-a", "table-a", "tenant-a", "file-a.tsdb"))
	_, ok = postingsObjectIdentity(root, "/cache/table-b/tenant-a/file-a.tsdb", "prefix-a")
	require.True(t, ok)
	common, ok := postingsObjectIdentity(root, "/cache/table-a/file-a.tsdb", "prefix-a")
	require.True(t, ok)
	require.Equal(t, objectIdentity("prefix-a", "table-a", "", "file-a.tsdb"), common)
	for _, tc := range []struct {
		path   string
		prefix string
	}{
		{"/cache/table-a/file-a.tsdb", "prefix-b"},
		{"/cache/table-b/file-a.tsdb", "prefix-a"},
		{"/cache/table-a/file-b.tsdb", "prefix-a"},
		{"/cache/table-a/tenant-a/file-a.tsdb", "prefix-a"},
		{"/cache/table-a/common/file-a.tsdb", "prefix-a"},
	} {
		other, ok := postingsObjectIdentity(root, tc.path, tc.prefix)
		require.True(t, ok)
		require.NotEqual(t, common, other, "common identity must distinguish prefix, table, filename and per-tenant objects")
	}
	for _, path := range []string{
		root,
		"/cache/file-a.tsdb",
		"/other/table-a/file-a.tsdb",
		"/cache/../other/table-a/file-a.tsdb",
	} {
		_, ok := postingsObjectIdentity(root, path, "prefix-a")
		require.False(t, ok, "invalid path: %s", path)
	}
	_, ok = postingsObjectIdentity(root, "/other/table-a/tenant-a/file-a.tsdb", "prefix-a")
	require.False(t, ok)
	_, ok = postingsObjectIdentity(root, "/cache/extra/table-a/tenant-a/file-a.tsdb", "prefix-a")
	require.False(t, ok)
}

type postingsReaderSpy struct {
	calls   int
	filters []index.FingerprintFilter
}

func (r *postingsReaderSpy) Bounds() (int64, int64) { return 0, math.MaxInt64 }
func (r *postingsReaderSpy) Checksum() uint32       { return 0 }
func (r *postingsReaderSpy) LabelValues(string, ...*labels.Matcher) ([]string, error) {
	return nil, nil
}
func (r *postingsReaderSpy) Postings(_ string, filter index.FingerprintFilter, _ ...string) (index.Postings, error) {
	r.calls++
	r.filters = append(r.filters, filter)
	refs := []storage.SeriesRef{1, 3}
	if filter != nil {
		filtered := refs[:0]
		for _, ref := range refs {
			if filter.Match(model.Fingerprint(ref)) {
				filtered = append(filtered, ref)
			}
		}
		refs = filtered
	}
	return index.NewListPostings(refs), nil
}
func (r *postingsReaderSpy) LabelNames(...*labels.Matcher) ([]string, error) { return nil, nil }
func (r *postingsReaderSpy) NewSeriesScan() index.SeriesScan                 { return nil }
func (r *postingsReaderSpy) Close() error                                    { return nil }

type testFingerprintFilter struct{ from, through model.Fingerprint }

func (f testFingerprintFilter) Match(fp model.Fingerprint) bool {
	return fp >= f.from && fp < f.through
}

func (f testFingerprintFilter) GetFromThrough() (model.Fingerprint, model.Fingerprint) {
	return f.from, f.through
}

func TestTSDBIndexPostingsCachePreservesShardPushdown(t *testing.T) {
	reader := &postingsReaderSpy{}
	postingsCache := newPostingsCache(&postingsTestCache{}, "test", prometheus.NewRegistry(), log.NewNopLogger())
	idx := &TSDBIndex{reader: reader, postingsCache: postingsCache, postingsID: "file"}
	m := labels.MustNewMatcher(labels.MatchEqual, "app", "api")
	low := testFingerprintFilter{from: 0, through: 2}
	high := testFingerprintFilter{from: 2, through: 4}
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

	require.Equal(t, []storage.SeriesRef{1}, query(low, 0, 10))
	require.Equal(t, []storage.SeriesRef{1}, query(low, 100, 200))
	require.Equal(t, 1, reader.calls)
	require.Equal(t, []storage.SeriesRef{3}, query(high, 0, 10))
	require.Equal(t, []storage.SeriesRef{3}, query(high, 100, 200))
	require.Equal(t, 2, reader.calls)
	lowFrom, lowThrough := low.GetFromThrough()
	gotLowFrom, gotLowThrough := reader.filters[0].GetFromThrough()
	require.Equal(t, lowFrom, gotLowFrom)
	require.Equal(t, lowThrough, gotLowThrough)
	highFrom, highThrough := high.GetFromThrough()
	gotHighFrom, gotHighThrough := reader.filters[1].GetFromThrough()
	require.Equal(t, highFrom, gotHighFrom)
	require.Equal(t, highThrough, gotHighThrough)

	uncached := &TSDBIndex{reader: reader}
	var got []storage.SeriesRef
	err := uncached.forPostings(context.Background(), low, 0, 10, []*labels.Matcher{m}, func(p index.Postings) error {
		var err error
		got, err = index.ExpandPostings(p)
		return err
	})
	require.NoError(t, err)
	require.Equal(t, []storage.SeriesRef{1}, got)
	err = uncached.forPostings(context.Background(), low, 100, 200, []*labels.Matcher{m}, func(p index.Postings) error {
		_, err := index.ExpandPostings(p)
		return err
	})
	require.NoError(t, err)
	require.Equal(t, 4, reader.calls)
}

func TestCachedPostingsAvoidsRecomputation(t *testing.T) {
	c := newPostingsCache(&postingsTestCache{}, "test", prometheus.NewRegistry(), log.NewNopLogger())
	called := 0
	compute := func() (index.Postings, error) {
		called++
		return index.NewListPostings([]storage.SeriesRef{4, 8}), nil
	}

	p, err := c.cachedPostings(context.Background(), "key", compute)
	require.NoError(t, err)
	_, err = index.ExpandPostings(p)
	require.NoError(t, err)
	require.Equal(t, 1, called)

	p, err = c.cachedPostings(context.Background(), "key", func() (index.Postings, error) {
		called++
		return index.EmptyPostings(), nil
	})
	require.NoError(t, err)
	refs, err := index.ExpandPostings(p)
	require.NoError(t, err)
	require.Equal(t, []storage.SeriesRef{4, 8}, refs)
	require.Equal(t, 1, called)
}

var _ cache.Cache = (*postingsTestCache)(nil)
