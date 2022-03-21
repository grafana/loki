package tsdb

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/tsdb/index"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

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
			// should be excluded due to bounds checking
			Labels: mustParseLabels(`{foo="bar", bazz="bozz", bonk="borb"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  8,
					MaxTime:  9,
					Checksum: 4,
				},
			},
		},
	}

	index := BuildIndex(t, cases)

	t.Run("GetChunkRefs", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()
		cacheableIndex := NewCacheableIndex(index, cache)

		refs, err := cacheableIndex.GetChunkRefs(context.Background(), "fake", 2, 5, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		require.Nil(t, err)

		expected := []ChunkRef{
			{
				User:        "fake",
				Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash()),
				Start:       1,
				End:         10,
				Checksum:    3,
			},
			{
				User:        "fake",
				Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar"}`).Hash()),
				Start:       0,
				End:         3,
				Checksum:    0,
			},
			{
				User:        "fake",
				Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar"}`).Hash()),
				Start:       1,
				End:         4,
				Checksum:    1,
			},
			{
				User:        "fake",
				Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar"}`).Hash()),
				Start:       2,
				End:         5,
				Checksum:    2,
			},
		}
		require.Equal(t, expected, refs)
	})

	t.Run("Series", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()
		cacheableIndex := NewCacheableIndex(index, cache)

		xs, err := cacheableIndex.Series(context.Background(), "fake", 2, 5, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		require.Nil(t, err)
		expected := []Series{
			{
				Labels:      mustParseLabels(`{foo="bar", bazz="buzz"}`),
				Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash()),
			},
			{
				Labels:      mustParseLabels(`{foo="bar"}`),
				Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar"}`).Hash()),
			},
		}

		require.Equal(t, expected, xs)
	})

	t.Run("LabelNames", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()
		cacheableIndex := NewCacheableIndex(index, cache)

		// request data at the end of the tsdb range, but it should return all labels present
		xs, err := cacheableIndex.LabelNames(context.Background(), "fake", 8, 10)
		require.Nil(t, err)
		expected := []string{"bazz", "bonk", "foo"}

		require.Equal(t, expected, xs)
	})

	t.Run("LabelNamesWithMatchers", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()
		cacheableIndex := NewCacheableIndex(index, cache)

		// request data at the end of the tsdb range, but it should return all labels present
		xs, err := cacheableIndex.LabelNames(context.Background(), "fake", 8, 10, labels.MustNewMatcher(labels.MatchEqual, "bazz", "buzz"))
		require.NoError(t, err)
		expected := []string{"bazz", "foo"}

		require.Equal(t, expected, xs)
	})

	t.Run("LabelValues", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()
		cacheableIndex := NewCacheableIndex(index, cache)

		xs, err := cacheableIndex.LabelValues(context.Background(), "fake", 1, 2, "bazz")
		require.Nil(t, err)
		expected := []string{"bozz", "buzz"}

		require.Equal(t, expected, xs)

	})

	t.Run("LabelValuesWithMatchers", func(t *testing.T) {
		cache := cache.NewFifoCache(t.Name(), cache.FifoCacheConfig{MaxSizeItems: 20}, nil, log.NewNopLogger())
		defer cache.Stop()
		cacheableIndex := NewCacheableIndex(index, cache)

		xs, err := cacheableIndex.LabelValues(context.Background(), "fake", 1, 2, "bazz", labels.MustNewMatcher(labels.MatchEqual, "bonk", "borb"))
		require.Nil(t, err)
		expected := []string{"bozz"}

		require.Equal(t, expected, xs)
	})
}
