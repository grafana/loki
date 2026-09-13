package tsdb

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/golang/snappy"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type postingsTestCache struct {
	buf       []byte
	entries   map[string][]byte
	fetchErr  error
	storeErr  error
	storeCall int
}

func (c *postingsTestCache) Store(_ context.Context, keys []string, bufs [][]byte) error {
	c.storeCall++
	if c.storeErr != nil {
		return c.storeErr
	}
	c.buf = bufs[0]
	if c.entries == nil {
		c.entries = map[string][]byte{}
	}
	c.entries[keys[0]] = bufs[0]
	return nil
}

func (c *postingsTestCache) Fetch(_ context.Context, keys []string) ([]string, [][]byte, []string, error) {
	if c.fetchErr != nil {
		return nil, nil, nil, c.fetchErr
	}
	if c.buf == nil {
		return nil, nil, []string{"missing"}, nil
	}
	if c.entries != nil {
		buf, ok := c.entries[keys[0]]
		if !ok {
			return nil, nil, keys, nil
		}
		return keys, [][]byte{buf}, nil, nil
	}
	return []string{"found"}, [][]byte{c.buf}, nil, nil
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
	decoded, ok := decodePostings(key1, encodePostings(key1, refs), ^storage.SeriesRef(0))
	require.True(t, ok)
	require.Equal(t, refs, decoded)
	require.Nil(t, encodePostings(key1, []storage.SeriesRef{2, 1}))
	require.False(t, func() bool {
		_, valid := decodePostings(key1+"x", encodePostings(key1, refs), ^storage.SeriesRef(0))
		return valid
	}())
}

func TestCachedPostingsFailuresRecomputeAndEmptyResultsCache(t *testing.T) {
	c := &postingsTestCache{fetchErr: errors.New("fetch")}
	called := 0
	compute := func() (index.Postings, error) {
		called++
		return index.EmptyPostings(), nil
	}
	_, err := cachedPostings(context.Background(), c, "key", ^storage.SeriesRef(0), compute)
	require.NoError(t, err)
	require.Equal(t, 1, called)
	c.fetchErr = nil
	p, err := cachedPostings(context.Background(), c, "key", ^storage.SeriesRef(0), func() (index.Postings, error) {
		called++
		return index.NewListPostings(nil), nil
	})
	require.NoError(t, err)
	refs, err := index.ExpandPostings(p)
	require.NoError(t, err)
	require.Empty(t, refs)
	require.Equal(t, 1, called)

	c = &postingsTestCache{storeErr: errors.New("store")}
	_, err = cachedPostings(context.Background(), c, "key", ^storage.SeriesRef(0), compute)
	require.NoError(t, err)
	require.Equal(t, 2, called)
}

func TestCachedPostingsRejectsOversizedAndWrongPayload(t *testing.T) {
	key := strings.Repeat("k", maxPostingsCacheKeyBytes+1)
	c := &postingsTestCache{}
	called := 0
	_, err := cachedPostings(context.Background(), c, key, ^storage.SeriesRef(0), func() (index.Postings, error) {
		called++
		return index.EmptyPostings(), nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, called)
	require.False(t, func() bool {
		_, ok := decodePostings("key", []byte{1, 2, 3}, ^storage.SeriesRef(0))
		return ok
	}())
	encoded := encodePostings("key", []storage.SeriesRef{1})
	payload, err := snappy.Decode(nil, encoded)
	require.NoError(t, err)
	payload[0] = 2
	_, ok := decodePostings("key", snappy.Encode(nil, payload), ^storage.SeriesRef(0))
	require.False(t, ok)
	declaration := []byte{1}
	declaration = binary.AppendUvarint(declaration, maxPostingsCacheBytes+1)
	_, ok = decodePostings("key", snappy.Encode(nil, declaration), ^storage.SeriesRef(0))
	require.False(t, ok)
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
	_, ok = postingsObjectIdentity(root, "/cache/table-a/file-a.tsdb", "prefix-a")
	require.False(t, ok)
	_, ok = postingsObjectIdentity(root, "/other/table-a/tenant-a/file-a.tsdb", "prefix-a")
	require.False(t, ok)
}

type postingsReaderSpy struct {
	calls   int
	filters []index.FingerprintFilter
}

func (*postingsReaderSpy) Size() int64              { return 64 }
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
	postingsCache := &postingsTestCache{}
	idx := &TSDBIndex{reader: reader, postingsCache: postingsCache, postingsID: "file", postingsMaxRef: 3}
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

func TestTSDBIndexRejectsCachedRefBeyondFileBound(t *testing.T) {
	reader := &postingsReaderSpy{}
	filter := testFingerprintFilter{from: 0, through: 4}
	m := labels.MustNewMatcher(labels.MatchEqual, "app", "api")
	key := postingsKey("file", filter, []*labels.Matcher{m})
	encoded := encodePostings(key, []storage.SeriesRef{4})
	postingsCache := &postingsTestCache{buf: encoded, entries: map[string][]byte{cache.HashKey(key): encoded}}
	idx := &TSDBIndex{reader: reader, postingsCache: postingsCache, postingsID: "file", postingsMaxRef: 3}
	var refs []storage.SeriesRef
	err := idx.forPostings(context.Background(), filter, 0, 10, []*labels.Matcher{m}, func(p index.Postings) error {
		var err error
		refs, err = index.ExpandPostings(p)
		return err
	})
	require.NoError(t, err)
	require.Equal(t, []storage.SeriesRef{1, 3}, refs)
	require.Equal(t, 1, reader.calls)
}

func TestCachedPostingsAvoidsRecomputation(t *testing.T) {
	c := &postingsTestCache{}
	called := 0
	compute := func() (index.Postings, error) {
		called++
		return index.NewListPostings([]storage.SeriesRef{4, 8}), nil
	}

	p, err := cachedPostings(context.Background(), c, "key", ^storage.SeriesRef(0), compute)
	require.NoError(t, err)
	_, err = index.ExpandPostings(p)
	require.NoError(t, err)
	require.Equal(t, 1, called)

	p, err = cachedPostings(context.Background(), c, "key", ^storage.SeriesRef(0), func() (index.Postings, error) {
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
