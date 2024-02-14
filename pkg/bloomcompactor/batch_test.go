package bloomcompactor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

type dummyBlocksFetcher struct {
	count *atomic.Int32
}

func (f *dummyBlocksFetcher) FetchBlocks(_ context.Context, blocks []bloomshipper.BlockRef) ([]*bloomshipper.CloseableBlockQuerier, error) {
	f.count.Inc()
	return make([]*bloomshipper.CloseableBlockQuerier, len(blocks)), nil
}

func TestBatchedBlockLoader(t *testing.T) {
	ctx := context.Background()
	f := &dummyBlocksFetcher{count: atomic.NewInt32(0)}

	blocks := make([]bloomshipper.BlockRef, 25)
	blocksIter, err := newBatchedBlockLoader(ctx, f, blocks)
	require.NoError(t, err)

	var count int
	for blocksIter.Next() && blocksIter.Err() == nil {
		count++
	}

	require.Equal(t, len(blocks), count)
	require.Equal(t, int32(len(blocks)/blocksIter.batchSize+1), f.count.Load())
}

func TestBatchedLoader(t *testing.T) {
	errMapper := func(i int) (int, error) {
		return 0, errors.New("bzzt")
	}
	successMapper := func(i int) (int, error) {
		return i, nil
	}

	expired, cancel := context.WithCancel(context.Background())
	cancel()

	for _, tc := range []struct {
		desc      string
		ctx       context.Context
		batchSize int
		mapper    func(int) (int, error)
		err       bool
		inputs    [][]int
		exp       []int
	}{
		{
			desc:      "OneBatch",
			ctx:       context.Background(),
			batchSize: 2,
			mapper:    successMapper,
			err:       false,
			inputs:    [][]int{{0, 1}},
			exp:       []int{0, 1},
		},
		{
			desc:      "ZeroBatchSizeStillWorks",
			ctx:       context.Background(),
			batchSize: 0,
			mapper:    successMapper,
			err:       false,
			inputs:    [][]int{{0, 1}},
			exp:       []int{0, 1},
		},
		{
			desc:      "OneBatchLessThanFull",
			ctx:       context.Background(),
			batchSize: 2,
			mapper:    successMapper,
			err:       false,
			inputs:    [][]int{{0}},
			exp:       []int{0},
		},
		{
			desc:      "TwoBatches",
			ctx:       context.Background(),
			batchSize: 2,
			mapper:    successMapper,
			err:       false,
			inputs:    [][]int{{0, 1, 2, 3}},
			exp:       []int{0, 1, 2, 3},
		},
		{
			desc:      "MultipleBatchesMultipleLoaders",
			ctx:       context.Background(),
			batchSize: 2,
			mapper:    successMapper,
			err:       false,
			inputs:    [][]int{{0, 1}, {2}, {3, 4, 5}},
			exp:       []int{0, 1, 2, 3, 4, 5},
		},
		{
			desc:      "HandlesEmptyInputs",
			ctx:       context.Background(),
			batchSize: 2,
			mapper:    successMapper,
			err:       false,
			inputs:    [][]int{{0, 1, 2, 3}, nil, {4}},
			exp:       []int{0, 1, 2, 3, 4},
		},
		{
			desc:      "Timeout",
			ctx:       expired,
			batchSize: 2,
			mapper:    successMapper,
			err:       true,
			inputs:    [][]int{{0}},
		},
		{
			desc:      "MappingFailure",
			ctx:       context.Background(),
			batchSize: 2,
			mapper:    errMapper,
			err:       true,
			inputs:    [][]int{{0}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			fetchers := make([]Fetcher[int, int], 0, len(tc.inputs))
			for range tc.inputs {
				fetchers = append(
					fetchers,
					FetchFunc[int, int](func(ctx context.Context, xs []int) ([]int, error) {
						if ctx.Err() != nil {
							return nil, ctx.Err()
						}
						return xs, nil
					}),
				)
			}

			loader := newBatchedLoader[int, int, int](
				tc.ctx,
				fetchers,
				tc.inputs,
				tc.mapper,
				tc.batchSize,
			)

			got, err := v1.Collect[int](loader)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.exp, got)

		})
	}

}
