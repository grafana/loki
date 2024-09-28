package v1

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/util/encoding"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

// smallBlockOpts returns a set of block options that are suitable for testing
// characterized by small page sizes
func smallBlockOpts(v Version, enc compression.Encoding) BlockOptions {
	return BlockOptions{
		Schema: Schema{
			version:  v,
			encoding: enc,
		},
		SeriesPageSize: 100,
		BloomPageSize:  2 << 10,
		BlockSize:      0, // unlimited
	}
}

func setup(v Version) (BlockOptions, []SeriesWithBlooms, BlockWriter, BlockReader) {
	numSeries := 100
	data, _ := MkBasicSeriesWithBlooms(numSeries, 0, 0xffff, 0, 10000)
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)
	return smallBlockOpts(v, compression.EncNone), data, writer, reader
}

func TestV3Roundtrip(t *testing.T) {
	opts, sourceData, writer, reader := setup(V3)

	// SeriesWithBlooms holds an interator of blooms,
	// which will be exhausted after being consumed by the block builder
	// therefore we need a deepcopy of the original data, or - and that's easier to achieve -
	// we simply create the same data twice.
	_, unmodifiedData, _, _ := setup(V3)

	b, err := NewBlockBuilderV3(opts, writer)
	require.NoError(t, err)

	_, err = b.BuildFrom(v2.NewSliceIter(sourceData))
	require.NoError(t, err)

	// Ensure Equality
	block := NewBlock(reader, NewMetrics(nil))
	querier := NewBlockQuerier(block, &mempool.SimpleHeapAllocator{}, DefaultMaxPageSize).Iter()

	CompareIterators[SeriesWithBlooms, *SeriesWithBlooms](
		t,
		func(t *testing.T, a SeriesWithBlooms, b *SeriesWithBlooms) {
			require.Equal(t, a.Series.Series.Fingerprint, b.Series.Series.Fingerprint)
			require.ElementsMatch(t, a.Series.Series.Chunks, b.Series.Series.Chunks)
			bloomsA, err := v2.Collect(a.Blooms)
			require.NoError(t, err)
			bloomsB, err := v2.Collect(b.Blooms)
			require.NoError(t, err)

			require.Equal(t, 2, len(bloomsA))
			require.Equal(t, 2, len(bloomsB))

			var encA, encB encoding.Encbuf
			for i := range bloomsA {
				require.NoError(t, bloomsA[i].Encode(&encA))
				require.NoError(t, bloomsB[i].Encode(&encB))
				require.Equal(t, encA.Get(), encB.Get())
				encA.Reset()
				encB.Reset()
			}
		},
		v2.NewSliceIter(unmodifiedData),
		querier,
	)
}
