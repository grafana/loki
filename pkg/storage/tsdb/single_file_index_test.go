package tsdb

import (
	"context"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/tsdb/index"
)

func TestSingleIdx(t *testing.T) {
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
		refs, err := idx.GetChunkRefs(context.Background(), "fake", 1, 5, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
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
		xs, err := idx.Series(context.Background(), "fake", 8, 9, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		require.Nil(t, err)

		expected := []Series{
			{
				Labels: mustParseLabels(`{foo="bar", bazz="buzz"}`),
			},
		}
		require.Equal(t, expected, xs)
	})

	t.Run("LabelNames", func(t *testing.T) {
		// request data at the end of the tsdb range, but it should return all labels present
		ls, err := idx.LabelNames(context.Background(), "fake", 9, 10)
		require.Nil(t, err)
		require.Equal(t, []string{"bazz", "bonk", "foo"}, ls)
	})

	t.Run("LabelNamesWithMatchers", func(t *testing.T) {
		// request data at the end of the tsdb range, but it should return all labels present
		ls, err := idx.LabelNames(context.Background(), "fake", 9, 10, labels.MustNewMatcher(labels.MatchEqual, "bazz", "buzz"))
		require.Nil(t, err)
		require.Equal(t, []string{"bazz", "foo"}, ls)
	})

	t.Run("LabelValues", func(t *testing.T) {
		vs, err := idx.LabelValues(context.Background(), "fake", 9, 10, "foo")
		require.Nil(t, err)
		require.Equal(t, []string{"bar", "bard"}, vs)
	})

	t.Run("LabelValuesWithMatchers", func(t *testing.T) {
		vs, err := idx.LabelValues(context.Background(), "fake", 9, 10, "foo", labels.MustNewMatcher(labels.MatchEqual, "bazz", "buzz"))
		require.Nil(t, err)
		require.Equal(t, []string{"bar"}, vs)
	})
}
