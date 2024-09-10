package v1

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/util/encoding"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

// smallBlockOpts returns a set of block options that are suitable for testing
// characterized by small page sizes
func smallBlockOpts(v Version, enc chunkenc.Encoding) BlockOptions {
	return BlockOptions{
		Schema: Schema{
			version:     v,
			encoding:    enc,
			nGramLength: 4,
			nGramSkip:   0,
		},
		SeriesPageSize: 100,
		BloomPageSize:  2 << 10,
		BlockSize:      0, // unlimited
	}
}

func setup(v Version) (BlockOptions, []SeriesWithLiteralBlooms, BlockWriter, BlockReader) {
	numSeries := 100
	data, _ := MkBasicSeriesWithLiteralBlooms(numSeries, 0, 0xffff, 0, 10000)
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)
	return smallBlockOpts(v, chunkenc.EncNone), data, writer, reader
}

func TestV3Roundtrip(t *testing.T) {
	opts, data, writer, reader := setup(V3)

	data, err := v2.Collect(
		v2.NewMapIter[SeriesWithLiteralBlooms, SeriesWithLiteralBlooms](
			v2.NewSliceIter(data),
			func(swlb SeriesWithLiteralBlooms) SeriesWithLiteralBlooms {
				return SeriesWithLiteralBlooms{
					Series: swlb.Series,
					// hack(owen-d): data currently only creates one bloom per series, but I want to test multiple.
					// we're not checking the contents here, so ensuring the same bloom is used twice is fine.
					Blooms: []*Bloom{swlb.Blooms[0], swlb.Blooms[0]},
				}
			},
		),
	)
	require.NoError(t, err)

	b, err := NewBlockBuilderV3(opts, writer)
	require.NoError(t, err)

	mapped := v2.NewMapIter[SeriesWithLiteralBlooms](
		v2.NewSliceIter(data),
		func(s SeriesWithLiteralBlooms) SeriesWithBlooms {
			return s.SeriesWithBlooms()
		},
	)

	_, err = b.BuildFrom(mapped)
	require.NoError(t, err)

	// Ensure Equality
	block := NewBlock(reader, NewMetrics(nil))
	querier := NewBlockQuerier(block, &mempool.SimpleHeapAllocator{}, DefaultMaxPageSize).Iter()

	CompareIterators[SeriesWithLiteralBlooms, *SeriesWithBlooms](
		t,
		func(t *testing.T, a SeriesWithLiteralBlooms, b *SeriesWithBlooms) {
			require.Equal(t, a.Series, b.Series) // ensure series equality
			bs, err := v2.Collect(b.Blooms)
			require.NoError(t, err)

			// ensure we only have one bloom in v1
			require.Equal(t, 2, len(a.Blooms))
			require.Equal(t, 2, len(bs))

			var encA, encB encoding.Encbuf
			for i := range a.Blooms {
				require.NoError(t, a.Blooms[i].Encode(&encA))
				require.NoError(t, bs[i].Encode(&encB))
				require.Equal(t, encA.Get(), encB.Get())
				encA.Reset()
				encB.Reset()
			}
		},
		v2.NewSliceIter(data),
		querier,
	)
}
