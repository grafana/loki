package bloomcompactor

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

func blocksFromSchema(t *testing.T, n int, options v1.BlockOptions) (res []*v1.Block, data []v1.SeriesWithBloom) {
	return blocksFromSchemaWithRange(t, n, options, 0, 0xffff)
}

// splits 100 series across `n` non-overlapping blocks.
// uses options to build blocks with.
func blocksFromSchemaWithRange(t *testing.T, n int, options v1.BlockOptions, fromFP, throughFp model.Fingerprint) (res []*v1.Block, data []v1.SeriesWithBloom) {
	if 100%n != 0 {
		panic("100 series must be evenly divisible by n")
	}

	numSeries := 100
	numKeysPerSeries := 10000
	data, _ = v1.MkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, fromFP, throughFp, 0, 10000)

	seriesPerBlock := numSeries / n

	for i := 0; i < n; i++ {
		// references for linking in memory reader+writer
		indexBuf := bytes.NewBuffer(nil)
		bloomsBuf := bytes.NewBuffer(nil)
		writer := v1.NewMemoryBlockWriter(indexBuf, bloomsBuf)
		reader := v1.NewByteReader(indexBuf, bloomsBuf)

		builder, err := v1.NewBlockBuilder(
			options,
			writer,
		)
		require.Nil(t, err)

		itr := v1.NewSliceIter[v1.SeriesWithBloom](data[i*seriesPerBlock : (i+1)*seriesPerBlock])
		_, err = builder.BuildFrom(itr)
		require.Nil(t, err)

		res = append(res, v1.NewBlock(reader))
	}

	return res, data
}

// doesn't actually load any chunks
type dummyChunkLoader struct{}

func (dummyChunkLoader) Load(_ context.Context, _ string, series *v1.Series) (*ChunkItersByFingerprint, error) {
	return &ChunkItersByFingerprint{
		fp:  series.Fingerprint,
		itr: v1.NewEmptyIter[v1.ChunkRefWithIter](),
	}, nil
}

func dummyBloomGen(opts v1.BlockOptions, store v1.Iterator[*v1.Series], blocks []*v1.Block) *SimpleBloomGenerator {
	bqs := make([]*bloomshipper.CloseableBlockQuerier, 0, len(blocks))
	for _, b := range blocks {
		bqs = append(bqs, &bloomshipper.CloseableBlockQuerier{
			BlockQuerier: v1.NewBlockQuerier(b),
		})
	}
	blocksIter := v1.NewCloseableIterator(v1.NewSliceIter(bqs))

	return NewSimpleBloomGenerator(
		"fake",
		opts,
		store,
		dummyChunkLoader{},
		blocksIter,
		func() (v1.BlockWriter, v1.BlockReader) {
			indexBuf := bytes.NewBuffer(nil)
			bloomsBuf := bytes.NewBuffer(nil)
			return v1.NewMemoryBlockWriter(indexBuf, bloomsBuf), v1.NewByteReader(indexBuf, bloomsBuf)
		},
		NewMetrics(nil, v1.NewMetrics(nil)),
		log.NewNopLogger(),
	)
}

func TestSimpleBloomGenerator(t *testing.T) {
	const maxBlockSize = 100 << 20 // 100MB
	for _, tc := range []struct {
		desc                                   string
		fromSchema, toSchema                   v1.BlockOptions
		sourceBlocks, numSkipped, outputBlocks int
	}{
		{
			desc:         "SkipsIncompatibleSchemas",
			fromSchema:   v1.NewBlockOptions(3, 0, maxBlockSize),
			toSchema:     v1.NewBlockOptions(4, 0, maxBlockSize),
			sourceBlocks: 2,
			numSkipped:   2,
			outputBlocks: 1,
		},
		{
			desc:         "CombinesBlocks",
			fromSchema:   v1.NewBlockOptions(4, 0, maxBlockSize),
			toSchema:     v1.NewBlockOptions(4, 0, maxBlockSize),
			sourceBlocks: 2,
			numSkipped:   0,
			outputBlocks: 1,
		},
		{
			desc:         "MaxBlockSize",
			fromSchema:   v1.NewBlockOptions(4, 0, maxBlockSize),
			toSchema:     v1.NewBlockOptions(4, 0, 1<<10), // 1KB
			sourceBlocks: 2,
			numSkipped:   0,
			outputBlocks: 3,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			sourceBlocks, data := blocksFromSchema(t, tc.sourceBlocks, tc.fromSchema)
			storeItr := v1.NewMapIter[v1.SeriesWithBloom, *v1.Series](
				v1.NewSliceIter[v1.SeriesWithBloom](data),
				func(swb v1.SeriesWithBloom) *v1.Series {
					return swb.Series
				},
			)

			gen := dummyBloomGen(tc.toSchema, storeItr, sourceBlocks)
			skipped, _, results, err := gen.Generate(context.Background())
			require.Nil(t, err)
			require.Equal(t, tc.numSkipped, len(skipped))

			var outputBlocks []*v1.Block
			for results.Next() {
				outputBlocks = append(outputBlocks, results.At())
			}
			require.Equal(t, tc.outputBlocks, len(outputBlocks))

			// Check all the input series are present in the output blocks.
			expectedRefs := v1.PointerSlice(data)
			outputRefs := make([]*v1.SeriesWithBloom, 0, len(data))
			for _, block := range outputBlocks {
				bq := block.Querier()
				for bq.Next() {
					outputRefs = append(outputRefs, bq.At())
				}
			}
			require.Equal(t, len(expectedRefs), len(outputRefs))
			for i := range expectedRefs {
				require.Equal(t, expectedRefs[i].Series, outputRefs[i].Series)
			}
		})
	}
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
