package v1

import (
	"bytes"
	"testing"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/util/encoding"
	"github.com/stretchr/testify/require"
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

// Tests v1 format by encoding a block into v1 then decoding it back and comparing the results
// to the source data.
// NB(owen-d): This also tests that the block querier can "up cast" the v1 format to the v2 format
// in the sense that v1 uses a single bloom per series and v2 uses multiple blooms per series and therefore
// v1 can be interpreted as v2 with a single bloom per series.
func TestV1RoundTrip(t *testing.T) {
	opts, data, writer, reader := setup(V1)
	b, err := NewBlockBuilderV1(opts, writer)
	require.NoError(t, err)

	mapped := NewMapIter[SeriesWithLiteralBlooms](
		NewSliceIter(data),
		func(s SeriesWithLiteralBlooms) SeriesWithBloom {
			return SeriesWithBloom{
				Series: s.Series,
				Bloom:  s.Blooms[0],
			}
		},
	)

	_, err = b.BuildFrom(mapped)
	require.NoError(t, err)

	// Ensure Equality
	block := NewBlock(reader, NewMetrics(nil))
	querier := NewBlockQuerier(block, false, DefaultMaxPageSize).Iter()

	CompareIterators[SeriesWithLiteralBlooms, *SeriesWithBlooms](
		t,
		func(t *testing.T, a SeriesWithLiteralBlooms, b *SeriesWithBlooms) {
			require.Equal(t, a.Series, b.Series) // ensure series equality
			bs, err := Collect(b.Blooms)
			require.NoError(t, err)

			// ensure we only have one bloom in v1
			require.Equal(t, 1, len(a.Blooms))
			require.Equal(t, 1, len(bs))

			var encA, encB encoding.Encbuf
			require.NoError(t, a.Blooms[0].Encode(&encA))
			require.NoError(t, bs[0].Encode(&encB))

			require.Equal(t, encA.Get(), encB.Get())
		},
		NewSliceIter(data),
		querier,
	)
}

func TestV2Roundtrip(t *testing.T) {
	opts, data, writer, reader := setup(V2)

	data, err := Collect(
		NewMapIter[SeriesWithLiteralBlooms, SeriesWithLiteralBlooms](
			NewSliceIter(data),
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

	b, err := NewBlockBuilderV2(opts, writer)
	require.NoError(t, err)

	mapped := NewMapIter[SeriesWithLiteralBlooms](
		NewSliceIter(data),
		func(s SeriesWithLiteralBlooms) SeriesWithBlooms {
			return s.SeriesWithBlooms()
		},
	)

	_, err = b.BuildFrom(mapped)
	require.NoError(t, err)

	// Ensure Equality
	block := NewBlock(reader, NewMetrics(nil))
	querier := NewBlockQuerier(block, false, DefaultMaxPageSize).Iter()

	CompareIterators[SeriesWithLiteralBlooms, *SeriesWithBlooms](
		t,
		func(t *testing.T, a SeriesWithLiteralBlooms, b *SeriesWithBlooms) {
			require.Equal(t, a.Series, b.Series) // ensure series equality
			bs, err := Collect(b.Blooms)
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
		NewSliceIter(data),
		querier,
	)
}
