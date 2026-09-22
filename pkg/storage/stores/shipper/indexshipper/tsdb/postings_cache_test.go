package tsdb

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
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

func (*postingsTestCache) Stop() {}

func (*postingsTestCache) GetCacheType() stats.CacheType { return stats.IndexCache }

func TestPostingsCacheCanonicalKey(t *testing.T) {
	m1 := labels.MustNewMatcher(labels.MatchEqual, "app", "api")
	m2 := labels.MustNewMatcher(labels.MatchRegexp, "cluster", "prod|staging")
	key1 := postingsKey(objectIdentity("prefix", "table", "tenant", "file.tsdb"), nil, []*labels.Matcher{m1, m2})
	key2 := postingsKey(objectIdentity("prefix", "table", "tenant", "file.tsdb"), nil, []*labels.Matcher{m2, m1})
	require.Equal(t, key1, key2)
	require.NotEqual(t,
		postingsKey("id", nil, []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "~api")}),
		postingsKey("id", nil, []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "app", "api")}),
	)
	require.NotEqual(t,
		objectIdentity("prefix", "table", "tenant", "file.tsdb"),
		objectIdentity("prefix", "table", "other", "file.tsdb"),
	)
	require.NotEqual(t,
		postingsKey("id", nil, []*labels.Matcher{m1}),
		postingsKey("id", index.ShardAnnotation{Shard: 0, Of: 2}, []*labels.Matcher{m1}),
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
	file, err := openShippableTSDBWithPostingsCache(path, index.MmapOptions{}, c, "prefix", root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	idx := NewMultiTenantIndex(file.(*TSDBFile))
	matcher := labels.MustNewMatcher(labels.MatchEqual, "app", "api")
	first, err := idx.GetChunkRefs(context.Background(), "tenant-a", 0, 10, nil, nil, matcher)
	require.NoError(t, err)
	second, err := idx.GetChunkRefs(context.Background(), "tenant-a", 0, 10, nil, nil, matcher)
	require.NoError(t, err)

	require.Equal(t, first, second)
	require.Equal(t, 2, backend.fetches, "queries must fetch postings through the cache attached by the opener")
	require.Equal(t, 1, backend.stores, "only the first query for each tenant should store postings")
	require.Equal(t, 1, backend.hits, "the repeated query must hit cached postings")

	_, err = idx.GetChunkRefs(context.Background(), "tenant-b", 0, 10, nil, nil, matcher)
	require.NoError(t, err)
	require.Len(t, backend.entries, 2, "tenant matchers must produce separate cache entries")
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
}

func TestTSDBIndexPostingsCacheDisabled(t *testing.T) {
	reader := &postingsReaderSpy{}
	idx := &TSDBIndex{reader: reader}
	m := labels.MustNewMatcher(labels.MatchEqual, "app", "api")
	for range 2 {
		err := idx.forPostings(context.Background(), nil, 0, 10, []*labels.Matcher{m}, func(p index.Postings) error {
			return nil
		})
		require.NoError(t, err)
	}
	require.Equal(t, 2, reader.calls)
}

func TestCachedPostingsAvoidsRecomputation(t *testing.T) {
	c := newPostingsCache(&postingsTestCache{}, "test", prometheus.NewRegistry(), log.NewNopLogger())
	called := 0
	compute := func() (index.Postings, error) {
		called++
		return index.NewListPostings([]storage.SeriesRef{4, 8}), nil
	}

	_, err := c.cachedPostings(context.Background(), "key", compute)
	require.NoError(t, err)

	require.Equal(t, 1, called)

	_, err = c.cachedPostings(context.Background(), "key", func() (index.Postings, error) {
		called++
		return index.EmptyPostings(), nil
	})
	require.NoError(t, err)

	require.NoError(t, err)
	require.Equal(t, 1, called)
}

var _ cache.Cache = (*postingsTestCache)(nil)
