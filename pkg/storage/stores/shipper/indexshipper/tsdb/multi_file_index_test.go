package tsdb

import (
	"context"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestMultiIndex(t *testing.T) {
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

	// group 5 indices together, all with duplicate data
	n := 5
	var indices []Index
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		indices = append(indices, BuildIndex(t, dir, cases))
	}

	idx := NewMultiIndex(IndexSlice(indices))

	t.Run("GetChunkRefs", func(t *testing.T) {
		refs, err := idx.GetChunkRefs(context.Background(), "fake", 2, 5, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		require.Nil(t, err)

		expected := []ChunkRef{
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
			{
				User:        "fake",
				Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash()),
				Start:       1,
				End:         10,
				Checksum:    3,
			},
		}
		require.Equal(t, expected, refs)
	})

	t.Run("Series", func(t *testing.T) {
		xs, err := idx.Series(context.Background(), "fake", 2, 5, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		require.Nil(t, err)
		expected := []Series{
			{
				Labels:      mustParseLabels(`{foo="bar"}`),
				Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar"}`).Hash()),
			},
			{
				Labels:      mustParseLabels(`{foo="bar", bazz="buzz"}`),
				Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash()),
			},
		}

		require.Equal(t, expected, xs)
	})

	t.Run("LabelNames", func(t *testing.T) {
		// request data at the end of the tsdb range, but it should return all labels present
		xs, err := idx.LabelNames(context.Background(), "fake", 8, 10)
		require.Nil(t, err)
		expected := []string{"bazz", "bonk", "foo"}

		require.Equal(t, expected, xs)
	})

	t.Run("LabelNamesWithMatchers", func(t *testing.T) {
		// request data at the end of the tsdb range, but it should return all labels present
		xs, err := idx.LabelNames(context.Background(), "fake", 8, 10, labels.MustNewMatcher(labels.MatchEqual, "bazz", "buzz"))
		require.Nil(t, err)
		expected := []string{"bazz", "foo"}

		require.Equal(t, expected, xs)
	})

	t.Run("LabelValues", func(t *testing.T) {
		xs, err := idx.LabelValues(context.Background(), "fake", 1, 2, "bazz")
		require.Nil(t, err)
		expected := []string{"bozz", "buzz"}

		require.Equal(t, expected, xs)
	})

	t.Run("LabelValuesWithMatchers", func(t *testing.T) {
		xs, err := idx.LabelValues(context.Background(), "fake", 1, 2, "bazz", labels.MustNewMatcher(labels.MatchEqual, "bonk", "borb"))
		require.Nil(t, err)
		expected := []string{"bozz"}

		require.Equal(t, expected, xs)
	})
}
