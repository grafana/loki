package tsdb

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/tsdb/index"
)

// buildIndexB is a copy of BuildIndex that expects a *testing.B instead of a *testing.T.
//
// Once we fully migrate to Go v1.18 we can rewrite this to use generics.
func buildIndexB(t *testing.B, cases []LoadableSeries) *TSDBIndex {
	dir := t.TempDir()
	b := index.NewBuilder()

	for _, s := range cases {
		b.AddSeries(s.Labels, s.Chunks)
	}

	require.Nil(t, b.Build(context.Background(), dir))

	reader, err := index.NewFileReader(dir)
	require.Nil(t, err)

	return NewTSDBIndex(reader)
}

func BenchmarkCacheableIndex_Buffering(b *testing.B) {
	cases := []LoadableSeries{
		{
			Labels: mustParseLabels(`{foo="bar"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  0,
					MaxTime:  3,
					Checksum: 0,
				},
				{
					MinTime:  1,
					MaxTime:  4,
					Checksum: 1,
				},
				{
					MinTime:  2,
					MaxTime:  5,
					Checksum: 2,
				},
			},
		},
		{
			Labels: mustParseLabels(`{foo="bar", bazz="buzz"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  1,
					MaxTime:  10,
					Checksum: 3,
				},
			},
		},
		{
			Labels: mustParseLabels(`{foo="bard", bazz="bozz", bonk="borb"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  1,
					MaxTime:  7,
					Checksum: 4,
				},
			},
		},
	}

	b.Run("GetChunkRef", func(b *testing.B) {
		expected := []ChunkRef{
			{
				User:        "fake",
				Fingerprint: &Fingerprint{int64(mustParseLabels(`{foo="bar"}`).Hash())},
				Start:       0,
				End:         3,
				Checksum:    0,
			},
			{
				User:        "fake",
				Fingerprint: &Fingerprint{int64(mustParseLabels(`{foo="bar"}`).Hash())},
				Start:       1,
				End:         4,
				Checksum:    1,
			},
			{
				User:        "fake",
				Fingerprint: &Fingerprint{int64(mustParseLabels(`{foo="bar"}`).Hash())},
				Start:       2,
				End:         5,
				Checksum:    2,
			},
			{
				User:        "fake",
				Fingerprint: &Fingerprint{int64(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash())},
				Start:       1,
				End:         10,
				Checksum:    3,
			},
		}

		cache := cache.NewFifoCache(b.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		idx := buildIndexB(b, cases)
		defer cache.Stop()
		l := log.NewNopLogger()
		nm := NewCacheMetrics(nil)
		cacheableIndex := NewCacheableIndex(l, idx, cache, nm)

		type getChunkRefCall func(userID string, from model.Time, through model.Time, shard *index.ShardAnnotation, matcher *labels.Matcher) ([]ChunkRef, error)

		ctx := context.Background()
		var emptyBuf []ChunkRef
		buf := make([]ChunkRef, 0, len(expected))
		scenarios := []struct {
			name       string
			invocation getChunkRefCall
		}{
			{
				name: "no buffer allocation",
				invocation: func(userID string, from, through model.Time, shard *index.ShardAnnotation, matcher *labels.Matcher) ([]ChunkRef, error) {
					return cacheableIndex.GetChunkRefs(ctx, userID, from, through, nil, shard, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
				},
			},
			{
				name: "empty buffer",
				invocation: func(userID string, from, through model.Time, shard *index.ShardAnnotation, matcher *labels.Matcher) ([]ChunkRef, error) {
					return cacheableIndex.GetChunkRefs(ctx, userID, from, through, emptyBuf, shard, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
				},
			},
			{
				name: "preallocated buffer",
				invocation: func(userID string, from, through model.Time, shard *index.ShardAnnotation, matcher *labels.Matcher) ([]ChunkRef, error) {
					return cacheableIndex.GetChunkRefs(ctx, userID, from, through, buf, shard, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
				},
			},
		}

		matchers := labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")
		for _, scenario := range scenarios {
			b.Run(scenario.name, func(b *testing.B) {
				for n := 0; n < b.N; n++ {
					emptyBuf = []ChunkRef{}
					got, err := scenario.invocation("fake", 1, 5, nil, matchers)
					require.NoError(b, err)
					require.Equal(b, expected, got)
				}
			})
		}
	})

	b.Run("Series", func(b *testing.B) {
		expected := []Series{
			{
				Labels:      labelsToLabelsProto(mustParseLabels(`{foo="bar", bazz="buzz"}`), nil),
				Fingerprint: Fingerprint{int64(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash())},
			},
		}

		cache := cache.NewFifoCache(b.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		idx := buildIndexB(b, cases)
		defer cache.Stop()
		l := log.NewNopLogger()
		nm := NewCacheMetrics(nil)
		cacheableIndex := NewCacheableIndex(l, idx, cache, nm)

		type seriesCall func(userID string, from model.Time, through model.Time, shard *index.ShardAnnotation, matcher *labels.Matcher) ([]Series, error)

		ctx := context.Background()
		var emptyBuf []Series
		buf := make([]Series, 0, len(expected))
		scenarios := []struct {
			name       string
			invocation seriesCall
		}{
			{
				name: "no buffer allocation",
				invocation: func(userID string, from, through model.Time, shard *index.ShardAnnotation, matcher *labels.Matcher) ([]Series, error) {
					return cacheableIndex.Series(ctx, userID, from, through, nil, shard, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
				},
			},
			{
				name: "empty buffer",
				invocation: func(userID string, from, through model.Time, shard *index.ShardAnnotation, matcher *labels.Matcher) ([]Series, error) {
					return cacheableIndex.Series(ctx, userID, from, through, emptyBuf, shard, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
				},
			},
			{
				name: "preallocated buffer",
				invocation: func(userID string, from, through model.Time, shard *index.ShardAnnotation, matcher *labels.Matcher) ([]Series, error) {
					return cacheableIndex.Series(ctx, userID, from, through, buf, shard, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
				},
			},
		}

		matchers := labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")
		for _, scenario := range scenarios {
			b.Run(scenario.name, func(b *testing.B) {
				for n := 0; n < b.N; n++ {
					emptyBuf = []Series{} // reset buffer everytime.
					got, err := scenario.invocation("fake", 8, 9, nil, matchers)
					require.NoError(b, err)
					require.Equal(b, expected, got)
				}
			})
		}
	})
}

func TestCacheableIndex(t *testing.T) {
	cases := []LoadableSeries{
		{
			Labels: mustParseLabels(`{foo="bar"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  0,
					MaxTime:  3,
					Checksum: 0,
				},
				{
					MinTime:  1,
					MaxTime:  4,
					Checksum: 1,
				},
				{
					MinTime:  2,
					MaxTime:  5,
					Checksum: 2,
				},
			},
		},
		{
			Labels: mustParseLabels(`{foo="bar", bazz="buzz"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  1,
					MaxTime:  10,
					Checksum: 3,
				},
			},
		},
		{
			Labels: mustParseLabels(`{foo="bard", bazz="bozz", bonk="borb"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  1,
					MaxTime:  7,
					Checksum: 4,
				},
			},
		},
	}

	idx := BuildIndex(t, cases)

	t.Run("GetChunkRefs", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()

		l := log.NewNopLogger()
		nm := NewCacheMetrics(nil)
		cacheableIndex := NewCacheableIndex(l, idx, cache, nm)

		refs, err := cacheableIndex.GetChunkRefs(context.Background(), "fake", 1, 5, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		require.NoError(t, err)

		expected := []ChunkRef{
			{
				User:        "fake",
				Fingerprint: &Fingerprint{int64(mustParseLabels(`{foo="bar"}`).Hash())},
				Start:       0,
				End:         3,
				Checksum:    0,
			},
			{
				User:        "fake",
				Fingerprint: &Fingerprint{int64(mustParseLabels(`{foo="bar"}`).Hash())},
				Start:       1,
				End:         4,
				Checksum:    1,
			},
			{
				User:        "fake",
				Fingerprint: &Fingerprint{int64(mustParseLabels(`{foo="bar"}`).Hash())},
				Start:       2,
				End:         5,
				Checksum:    2,
			},
			{
				User:        "fake",
				Fingerprint: &Fingerprint{int64(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash())},
				Start:       1,
				End:         10,
				Checksum:    3,
			},
		}
		require.Equal(t, expected, refs)

		// call again to ensure we get the same result
		refs, err = cacheableIndex.GetChunkRefs(context.Background(), "fake", 1, 5, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		require.NoError(t, err)
		require.Equal(t, expected, refs)
	})

	t.Run("ASDF", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()
		l := log.NewNopLogger()
		nm := NewCacheMetrics(nil)
		cacheableIndex := NewCacheableIndex(l, idx, cache, nm)

		shard := index.ShardAnnotation{
			Shard: 1,
			Of:    2,
		}
		shardedRefs, err := cacheableIndex.GetChunkRefs(context.Background(), "fake", 1, 5, nil, &shard, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))

		require.NoError(t, err)

		require.Equal(t, []ChunkRef{{
			User:        "fake",
			Fingerprint: &Fingerprint{int64(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash())},
			Start:       1,
			End:         10,
			Checksum:    3,
		}}, shardedRefs)

		// call again to ensure we get the same result
		shardedRefs, err = cacheableIndex.GetChunkRefs(context.Background(), "fake", 1, 5, nil, &shard, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))

		require.NoError(t, err)

		require.Equal(t, []ChunkRef{{
			User:        "fake",
			Fingerprint: &Fingerprint{int64(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash())},
			Start:       1,
			End:         10,
			Checksum:    3,
		}}, shardedRefs)
	})

	t.Run("Series", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()
		l := log.NewNopLogger()
		nm := NewCacheMetrics(nil)
		cacheableIndex := NewCacheableIndex(l, idx, cache, nm)

		xs, err := cacheableIndex.Series(context.Background(), "fake", 8, 9, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		require.NoError(t, err)

		expected := []Series{
			{
				Labels:      labelsToLabelsProto(mustParseLabels(`{foo="bar", bazz="buzz"}`), nil),
				Fingerprint: Fingerprint{int64(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash())},
			},
		}
		require.Equal(t, expected, xs)

		// call again to ensure we get the same result
		xs, err = cacheableIndex.Series(context.Background(), "fake", 8, 9, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		require.NoError(t, err)

		require.Equal(t, expected, xs)

		expected = []Series{
			{
				Labels:      labelsToLabelsProto(mustParseLabels(`{foo="bar"}`), nil),
				Fingerprint: Fingerprint{int64(mustParseLabels(`{foo="bar"}`).Hash())},
			},
			{
				Labels:      labelsToLabelsProto(mustParseLabels(`{foo="bar", bazz="buzz"}`), nil),
				Fingerprint: Fingerprint{int64(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash())},
			},
		}
		// See if we get the right result for two series
		xs, err = cacheableIndex.Series(context.Background(), "fake", 0, 10, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		require.NoError(t, err)

		require.Equal(t, expected, xs)

		// And confirm the cached result is correct.
		xs, err = cacheableIndex.Series(context.Background(), "fake", 0, 10, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		require.NoError(t, err)

		require.Equal(t, expected, xs)
	})

	t.Run("LabelNames", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()
		l := log.NewNopLogger()
		nm := NewCacheMetrics(nil)
		cacheableIndex := NewCacheableIndex(l, idx, cache, nm)

		// request data at the end of the tsdb range, but it should return all labels present
		xs, err := cacheableIndex.LabelNames(context.Background(), "fake", 8, 10)
		require.Nil(t, err)
		expected := []string{"bazz", "bonk", "foo"}

		require.Equal(t, expected, xs)

		// call again to ensure we get the same result
		xs, err = cacheableIndex.LabelNames(context.Background(), "fake", 8, 10)
		require.Nil(t, err)

		require.Equal(t, expected, xs)
	})

	t.Run("LabelNamesWithMatchers", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()
		l := log.NewNopLogger()
		nm := NewCacheMetrics(nil)
		cacheableIndex := NewCacheableIndex(l, idx, cache, nm)

		// request data at the end of the tsdb range, but it should return all labels present
		xs, err := cacheableIndex.LabelNames(context.Background(), "fake", 8, 10, labels.MustNewMatcher(labels.MatchEqual, "bazz", "buzz"))
		require.NoError(t, err)
		expected := []string{"bazz", "foo"}

		require.Equal(t, expected, xs)

		// call again to ensure we get the same result
		xs, err = cacheableIndex.LabelNames(context.Background(), "fake", 8, 10, labels.MustNewMatcher(labels.MatchEqual, "bazz", "buzz"))
		require.NoError(t, err)

		require.Equal(t, expected, xs)
	})

	t.Run("LabelValues", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()
		l := log.NewNopLogger()
		nm := NewCacheMetrics(nil)
		cacheableIndex := NewCacheableIndex(l, idx, cache, nm)

		xs, err := cacheableIndex.LabelValues(context.Background(), "fake", 1, 2, "bazz")
		require.Nil(t, err)
		expected := []string{"bozz", "buzz"}

		require.Equal(t, expected, xs)

		// call again to ensure we get the same result
		xs, err = cacheableIndex.LabelValues(context.Background(), "fake", 1, 2, "bazz")
		require.Nil(t, err)

		require.Equal(t, expected, xs)
	})

	t.Run("LabelValuesWithMatchers", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()
		l := log.NewNopLogger()
		nm := NewCacheMetrics(nil)
		cacheableIndex := NewCacheableIndex(l, idx, cache, nm)

		xs, err := cacheableIndex.LabelValues(context.Background(), "fake", 1, 2, "bazz", labels.MustNewMatcher(labels.MatchEqual, "bonk", "borb"))
		require.Nil(t, err)
		expected := []string{"bozz"}

		require.Equal(t, expected, xs)

		// call again to ensure we get the same result
		xs, err = cacheableIndex.LabelValues(context.Background(), "fake", 1, 2, "bazz", labels.MustNewMatcher(labels.MatchEqual, "bonk", "borb"))
		require.Nil(t, err)

		require.Equal(t, expected, xs)
	})

	t.Run("Metrics", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()
		l := log.NewNopLogger()
		nm := NewCacheMetrics(nil)
		cacheableIndex := NewCacheableIndex(l, idx, cache, nm)

		xs, err := cacheableIndex.LabelValues(context.Background(), "fake", 1, 2, "bazz", labels.MustNewMatcher(labels.MatchEqual, "bonk", "borb"))
		require.Nil(t, err)
		expected := []string{"bozz"}

		require.Equal(t, expected, xs)

		// At this point we should have a cache miss.
		require.Equal(t, testutil.ToFloat64(cacheableIndex.m.cacheMisses), float64(1))
		require.Equal(t, testutil.ToFloat64(cacheableIndex.m.cacheHits), float64(0))

		xs, err = cacheableIndex.LabelValues(context.Background(), "fake", 1, 2, "bazz", labels.MustNewMatcher(labels.MatchEqual, "bonk", "borb"))
		require.Nil(t, err)
		require.Equal(t, expected, xs)

		// Now we should have a cache hit.
		require.Equal(t, testutil.ToFloat64(cacheableIndex.m.cacheHits), float64(1))
		require.Equal(t, testutil.ToFloat64(cacheableIndex.m.cacheMisses), float64(1))
	})

	t.Run("CacheError", func(t *testing.T) {
		cache := NewMockFailCache()
		defer cache.Stop()
		l := log.NewNopLogger()
		nm := NewCacheMetrics(nil)
		cacheableIndex := NewCacheableIndex(l, idx, cache, nm)

		xs, err := cacheableIndex.LabelValues(context.Background(), "fake", 1, 2, "bazz", labels.MustNewMatcher(labels.MatchEqual, "bonk", "borb"))
		require.Nil(t, err)
		expected := []string{"bozz"}

		require.Equal(t, expected, xs)
	})
}

type mockFailCache struct {
	sync.Mutex
	cache map[string][]byte
}

func (m *mockFailCache) Store(_ context.Context, keys []string, bufs [][]byte) error {
	m.Lock()
	defer m.Unlock()
	for i := range keys {
		m.cache[keys[i]] = bufs[i]
	}
	return nil
}

func (m *mockFailCache) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	return nil, nil, nil, errors.New("the cache fetch failed")
}

func (m *mockFailCache) Stop() {
}

// NewMockFailCache makes a new MockCache.
func NewMockFailCache() cache.Cache {
	return &mockFailCache{
		cache: map[string][]byte{},
	}
}
