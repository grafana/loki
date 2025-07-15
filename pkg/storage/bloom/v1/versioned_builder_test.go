package v1

import (
	"bytes"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/util/encoding"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

// smallBlockOpts returns a set of block options that are suitable for testing
// characterized by small page sizes
func smallBlockOpts(v Version, enc compression.Codec) BlockOptions {
	return BlockOptions{
		Schema:         NewSchema(v, enc),
		SeriesPageSize: 4 << 10,
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
	return smallBlockOpts(v, compression.None), data, writer, reader
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

	CompareIterators(
		t,
		func(t *testing.T, a SeriesWithBlooms, b *SeriesWithBlooms) {
			require.Equal(t, a.Series.Fingerprint, b.Series.Fingerprint)
			require.ElementsMatch(t, a.Series.Chunks, b.Series.Chunks)
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

func seriesWithBlooms(nSeries int, fromFp, throughFp model.Fingerprint) []SeriesWithBlooms {
	series, _ := MkBasicSeriesWithBlooms(nSeries, fromFp, throughFp, 0, 10000)
	return series
}

func seriesWithoutBlooms(nSeries int, fromFp, throughFp model.Fingerprint) []SeriesWithBlooms {
	series := seriesWithBlooms(nSeries, fromFp, throughFp)

	// remove blooms from series
	for i := range series {
		series[i].Blooms = v2.NewEmptyIter[*Bloom]()
	}

	return series
}
func TestFullBlock(t *testing.T) {
	opts := smallBlockOpts(V3, compression.None)
	minBlockSize := opts.SeriesPageSize // 1 index page, 4KB
	const maxEmptySeriesPerBlock = 47
	for _, tc := range []struct {
		name         string
		maxBlockSize uint64
		series       []SeriesWithBlooms
		expected     []SeriesWithBlooms
	}{
		{
			name:         "only series without blooms",
			maxBlockSize: minBlockSize,
			// +1 so we test adding the last series that fills the block
			series:   seriesWithoutBlooms(maxEmptySeriesPerBlock+1, 0, 0xffff),
			expected: seriesWithoutBlooms(maxEmptySeriesPerBlock+1, 0, 0xffff),
		},
		{
			name:         "series without blooms and one with blooms",
			maxBlockSize: minBlockSize,
			series: append(
				seriesWithoutBlooms(maxEmptySeriesPerBlock, 0, 0x7fff),
				seriesWithBlooms(50, 0x8000, 0xffff)...,
			),
			expected: append(
				seriesWithoutBlooms(maxEmptySeriesPerBlock, 0, 0x7fff),
				seriesWithBlooms(1, 0x8000, 0x8001)...,
			),
		},
		{
			name:         "only one series with bloom",
			maxBlockSize: minBlockSize,
			series:       seriesWithBlooms(10, 0, 0xffff),
			expected:     seriesWithBlooms(1, 0, 1),
		},
		{
			name:         "one huge series with bloom and then series without",
			maxBlockSize: minBlockSize,
			series: append(
				seriesWithBlooms(1, 0, 1),
				seriesWithoutBlooms(100, 1, 0xffff)...,
			),
			expected: seriesWithBlooms(1, 0, 1),
		},
		{
			name:         "big block",
			maxBlockSize: 1 << 20, // 1MB
			series:       seriesWithBlooms(100, 0, 0xffff),
			expected:     seriesWithBlooms(100, 0, 0xffff),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			indexBuf := bytes.NewBuffer(nil)
			bloomsBuf := bytes.NewBuffer(nil)
			writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
			reader := NewByteReader(indexBuf, bloomsBuf)
			opts.BlockSize = tc.maxBlockSize

			b, err := NewBlockBuilderV3(opts, writer)
			require.NoError(t, err)

			_, err = b.BuildFrom(v2.NewSliceIter(tc.series))
			require.NoError(t, err)

			block := NewBlock(reader, NewMetrics(nil))
			querier := NewBlockQuerier(block, &mempool.SimpleHeapAllocator{}, DefaultMaxPageSize).Iter()

			CompareIterators(
				t,
				func(t *testing.T, a SeriesWithBlooms, b *SeriesWithBlooms) {
					require.Equal(t, a.Series.Fingerprint, b.Series.Fingerprint)
					require.ElementsMatch(t, a.Series.Chunks, b.Series.Chunks)
					bloomsA, err := v2.Collect(a.Blooms)
					require.NoError(t, err)
					bloomsB, err := v2.Collect(b.Blooms)
					require.NoError(t, err)
					require.Equal(t, len(bloomsB), len(bloomsA))
				},
				v2.NewSliceIter(tc.expected),
				querier,
			)
		})
	}
}
