package common

import (
	"context"
	"math"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type forSeriesTestImpl []*v1.Series

func (f forSeriesTestImpl) ForSeries(
	_ context.Context,
	_ string,
	_ index.FingerprintFilter,
	_ model.Time,
	_ model.Time,
	fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) bool,
	_ ...*labels.Matcher,
) error {
	for i := range f {
		unmapped := make([]index.ChunkMeta, 0, len(f[i].Chunks))
		for _, c := range f[i].Chunks {
			unmapped = append(unmapped, index.ChunkMeta{
				MinTime:  int64(c.From),
				MaxTime:  int64(c.Through),
				Checksum: c.Checksum,
			})
		}

		fn(nil, f[i].Fingerprint, unmapped)
	}
	return nil
}

func (f forSeriesTestImpl) Close() error {
	return nil
}

func TestTSDBSeriesIter(t *testing.T) {
	input := []*v1.Series{
		{
			Fingerprint: 1,
			Chunks: []v1.ChunkRef{
				{
					From:     0,
					Through:  1,
					Checksum: 2,
				},
				{
					From:     3,
					Through:  4,
					Checksum: 5,
				},
			},
		},
	}
	srcItr := v2.NewSliceIter(input)
	itr, err := NewTSDBSeriesIter(context.Background(), "", forSeriesTestImpl(input), v1.NewBounds(0, math.MaxUint64))
	require.NoError(t, err)

	v1.EqualIterators[*v1.Series](
		t,
		func(a, b *v1.Series) {
			require.Equal(t, a, b)
		},
		itr,
		srcItr,
	)
}

func TestTSDBSeriesIter_Expiry(t *testing.T) {
	t.Run("expires on creation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		itr, err := NewTSDBSeriesIter(ctx, "", forSeriesTestImpl{
			{}, // a single entry
		}, v1.NewBounds(0, math.MaxUint64))
		require.Error(t, err)
		require.False(t, itr.Next())
	})

	t.Run("expires during consumption", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		itr, err := NewTSDBSeriesIter(ctx, "", forSeriesTestImpl{
			{},
			{},
		}, v1.NewBounds(0, math.MaxUint64))
		require.NoError(t, err)

		require.True(t, itr.Next())
		require.NoError(t, itr.Err())

		cancel()
		require.False(t, itr.Next())
		require.Error(t, itr.Err())
	})

}
