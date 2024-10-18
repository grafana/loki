package builder

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

func TestBatchedLoader(t *testing.T) {
	t.Parallel()

	errMapper := func(_ int) (int, error) {
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

			loader := newBatchedLoader(
				tc.ctx,
				fetchers,
				tc.inputs,
				tc.mapper,
				tc.batchSize,
			)

			got, err := v2.Collect(loader)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.exp, got)

		})
	}
}

func TestOverlappingBlocksIter(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc string
		inp  []bloomshipper.BlockRef
		exp  int // expected groups
	}{
		{
			desc: "Empty",
			inp:  []bloomshipper.BlockRef{},
			exp:  0,
		},
		{
			desc: "NonOverlapping",
			inp: []bloomshipper.BlockRef{
				genBlockRef(0x0000, 0x00ff),
				genBlockRef(0x0100, 0x01ff),
				genBlockRef(0x0200, 0x02ff),
			},
			exp: 3,
		},
		{
			desc: "AllOverlapping",
			inp: []bloomshipper.BlockRef{
				genBlockRef(0x0000, 0x02ff), // |-----------|
				genBlockRef(0x0100, 0x01ff), //     |---|
				genBlockRef(0x0200, 0x02ff), //         |---|
			},
			exp: 1,
		},
		{
			desc: "PartialOverlapping",
			inp: []bloomshipper.BlockRef{
				genBlockRef(0x0000, 0x01ff), // group 1  |-------|
				genBlockRef(0x0100, 0x02ff), // group 1      |-------|
				genBlockRef(0x0200, 0x03ff), // group 1          |-------|
				genBlockRef(0x0200, 0x02ff), // group 1          |---|
			},
			exp: 1,
		},
		{
			desc: "PartialOverlapping",
			inp: []bloomshipper.BlockRef{
				genBlockRef(0x0000, 0x01ff), // group 1  |-------|
				genBlockRef(0x0100, 0x02ff), // group 1      |-------|
				genBlockRef(0x0100, 0x01ff), // group 1      |---|
				genBlockRef(0x0300, 0x03ff), // group 2              |---|
				genBlockRef(0x0310, 0x03ff), // group 2                |-|
			},
			exp: 2,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			it := overlappingBlocksIter(tc.inp)
			var overlapping [][]bloomshipper.BlockRef
			var i int
			for it.Next() && it.Err() == nil {
				require.NotNil(t, it.At())
				overlapping = append(overlapping, it.At())
				for _, r := range it.At() {
					t.Log(i, r)
				}
				i++
			}
			require.Equal(t, tc.exp, len(overlapping))
		})
	}
}

func genBlockRef(min, max model.Fingerprint) bloomshipper.BlockRef {
	bounds := v1.NewBounds(min, max)
	return bloomshipper.BlockRef{
		Ref: bloomshipper.Ref{
			Bounds: bounds,
		},
	}
}
