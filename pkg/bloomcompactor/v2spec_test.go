package bloomcompactor

import (
	"bytes"
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
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

	seriesPerBlock := 100 / n

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

func dummyBloomGen(opts v1.BlockOptions, store v1.Iterator[*v1.Series], blocks []*v1.Block) *SimpleBloomGenerator {
	return NewSimpleBloomGenerator(
		opts,
		store,
		blocks,
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
	for _, tc := range []struct {
		desc                     string
		fromSchema, toSchema     v1.BlockOptions
		sourceBlocks, numSkipped int
	}{
		{
			desc:         "SkipsIncompatibleSchemas",
			fromSchema:   v1.NewBlockOptions(3, 0),
			toSchema:     v1.NewBlockOptions(4, 0),
			sourceBlocks: 2,
			numSkipped:   2,
		},
		{
			desc:         "CombinesBlocks",
			fromSchema:   v1.NewBlockOptions(4, 0),
			toSchema:     v1.NewBlockOptions(4, 0),
			sourceBlocks: 2,
			numSkipped:   0,
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
			skipped, results, err := gen.Generate(context.Background())
			require.Nil(t, err)
			require.Equal(t, tc.numSkipped, len(skipped))

			require.True(t, results.Next())
			block := results.At()
			require.False(t, results.Next())

			refs := v1.PointerSlice[v1.SeriesWithBloom](data)

			v1.EqualIterators[*v1.SeriesWithBloom](
				t,
				func(a, b *v1.SeriesWithBloom) {
					// TODO(owen-d): better equality check
					// once chunk fetching is implemented
					require.Equal(t, a.Series, b.Series)
				},
				v1.NewSliceIter[*v1.SeriesWithBloom](refs),
				block.Querier(),
			)
		})
	}
}
