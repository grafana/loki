package builder

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

func blocksFromSchema(t *testing.T, n int, options v1.BlockOptions) (res []*v1.Block, data []v1.SeriesWithBlooms, refs []bloomshipper.BlockRef) {
	return blocksFromSchemaWithRange(t, n, options, 0, 0xffff)
}

// splits 100 series across `n` non-overlapping blocks.
// uses options to build blocks with.
func blocksFromSchemaWithRange(t *testing.T, n int, options v1.BlockOptions, fromFP, throughFp model.Fingerprint) (res []*v1.Block, data []v1.SeriesWithBlooms, refs []bloomshipper.BlockRef) {
	if 100%n != 0 {
		panic("100 series must be evenly divisible by n")
	}

	numSeries := 100
	data, _ = v1.MkBasicSeriesWithBlooms(numSeries, fromFP, throughFp, 0, 10000)

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

		minIdx, maxIdx := i*seriesPerBlock, (i+1)*seriesPerBlock

		itr := v2.NewSliceIter[v1.SeriesWithBlooms](data[minIdx:maxIdx])
		_, err = builder.BuildFrom(itr)
		require.Nil(t, err)

		res = append(res, v1.NewBlock(reader, v1.NewMetrics(nil)))
		ref := genBlockRef(data[minIdx].Series.Fingerprint, data[maxIdx-1].Series.Fingerprint)
		t.Log("create block", ref)
		refs = append(refs, ref)
	}

	return res, data, refs
}

// doesn't actually load any chunks
type dummyChunkLoader struct{}

func (dummyChunkLoader) Load(_ context.Context, _ string, series *v1.Series) *ChunkItersByFingerprint {
	return &ChunkItersByFingerprint{
		fp:  series.Fingerprint,
		itr: v2.NewEmptyIter[v1.ChunkRefWithIter](),
	}
}

func dummyBloomGen(t *testing.T, opts v1.BlockOptions, store v2.Iterator[*v1.Series], blocks []*v1.Block, refs []bloomshipper.BlockRef) *SimpleBloomGenerator {
	bqs := make([]*bloomshipper.CloseableBlockQuerier, 0, len(blocks))
	for i, b := range blocks {
		bqs = append(bqs, &bloomshipper.CloseableBlockQuerier{
			BlockRef:     refs[i],
			BlockQuerier: v1.NewBlockQuerier(b, &mempool.SimpleHeapAllocator{}, v1.DefaultMaxPageSize),
		})
	}

	fetcher := func(_ context.Context, refs []bloomshipper.BlockRef) ([]*bloomshipper.CloseableBlockQuerier, error) {
		res := make([]*bloomshipper.CloseableBlockQuerier, 0, len(refs))
		for _, ref := range refs {
			for _, bq := range bqs {
				if ref.Bounds.Equal(bq.Bounds) {
					res = append(res, bq)
				}
			}
		}
		t.Log("req", refs)
		t.Log("res", res)
		return res, nil
	}

	blocksIter := newBlockLoadingIter(context.Background(), refs, FetchFunc[bloomshipper.BlockRef, *bloomshipper.CloseableBlockQuerier](fetcher), 1)

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
		nil,
		v1.NewMetrics(nil),
		log.NewNopLogger(),
	)
}

func TestSimpleBloomGenerator(t *testing.T) {
	const maxBlockSize = 100 << 20 // 100MB
	for _, enc := range []chunkenc.Encoding{chunkenc.EncNone, chunkenc.EncGZIP, chunkenc.EncSnappy} {
		for _, tc := range []struct {
			desc                 string
			fromSchema, toSchema v1.BlockOptions
			overlapping          bool
		}{
			{
				desc:       "SkipsIncompatibleSchemas",
				fromSchema: v1.NewBlockOptions(enc, 3, 0, maxBlockSize, 0),
				toSchema:   v1.NewBlockOptions(enc, 4, 0, maxBlockSize, 0),
			},
			{
				desc:       "CombinesBlocks",
				fromSchema: v1.NewBlockOptions(enc, 4, 0, maxBlockSize, 0),
				toSchema:   v1.NewBlockOptions(enc, 4, 0, maxBlockSize, 0),
			},
		} {
			t.Run(fmt.Sprintf("%s/%s", tc.desc, enc), func(t *testing.T) {
				sourceBlocks, data, refs := blocksFromSchemaWithRange(t, 2, tc.fromSchema, 0x00000, 0x6ffff)
				storeItr := v2.NewMapIter[v1.SeriesWithBlooms, *v1.Series](
					v2.NewSliceIter[v1.SeriesWithBlooms](data),
					func(swb v1.SeriesWithBlooms) *v1.Series {
						return swb.Series
					},
				)

				gen := dummyBloomGen(t, tc.toSchema, storeItr, sourceBlocks, refs)
				results := gen.Generate(context.Background())

				var outputBlocks []*v1.Block
				for results.Next() {
					outputBlocks = append(outputBlocks, results.At())
				}
				// require.Equal(t, tc.outputBlocks, len(outputBlocks))

				// Check all the input series are present in the output blocks.
				expectedRefs := v1.PointerSlice(data)
				outputRefs := make([]*v1.SeriesWithBlooms, 0, len(data))
				for _, block := range outputBlocks {
					bq := v1.NewBlockQuerier(block, &mempool.SimpleHeapAllocator{}, v1.DefaultMaxPageSize).Iter()
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

}
