package v1

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
)

func EqualIterators[T any](t *testing.T, test func(a, b T), expected, actual Iterator[T]) {
	for expected.Next() {
		require.True(t, actual.Next())
		a, b := expected.At(), actual.At()
		test(a, b)
	}
	require.False(t, actual.Next())
	require.Nil(t, expected.Err())
	require.Nil(t, actual.Err())
}

func TestBlockBuilderRoundTrip(t *testing.T) {
	numSeries := 100
	numKeysPerSeries := 10000
	data, keys := mkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 0, 0xffff, 0, 10000)

	// references for linking in memory reader+writer
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	// directory for directory reader+writer
	tmpDir := t.TempDir()

	for _, tc := range []struct {
		desc               string
		writer             BlockWriter
		reader             BlockReader
		maxBlockSize       int
		iterHasPendingData bool
	}{
		{
			desc:   "in-memory",
			writer: NewMemoryBlockWriter(indexBuf, bloomsBuf),
			reader: NewByteReader(indexBuf, bloomsBuf),
		},
		{
			desc:   "directory",
			writer: NewDirectoryBlockWriter(tmpDir),
			reader: NewDirectoryBlockReader(tmpDir),
		},
		{
			desc:   "max block size",
			writer: NewDirectoryBlockWriter(tmpDir),
			reader: NewDirectoryBlockReader(tmpDir),
			// Set max block big enough to fit a bunch of series
			maxBlockSize:       500 << 10,
			iterHasPendingData: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			schema := Schema{
				version:     DefaultSchemaVersion,
				encoding:    chunkenc.EncSnappy,
				nGramLength: 10,
				nGramSkip:   2,
			}

			builder, err := NewBlockBuilder(
				BlockOptions{
					schema:         schema,
					SeriesPageSize: 100,
					BloomPageSize:  10 << 10,
					BlockSize:      tc.maxBlockSize,
				},
				tc.writer,
			)

			require.Nil(t, err)
			itr := NewPeekingIter[SeriesWithBloom](NewSliceIter[SeriesWithBloom](data))
			_, _, err = builder.BuildFrom(itr)
			require.Nil(t, err)

			firstPendingSeries, iterHasPendingData := itr.Peek()
			require.Equal(t, tc.iterHasPendingData, iterHasPendingData)

			processedData := data
			if iterHasPendingData {
				// If iter is not consumed, cut the data to the first pending series
				processedData = make([]SeriesWithBloom, 0, len(data))
				for _, d := range data {
					if d.Series.Fingerprint >= firstPendingSeries.Series.Fingerprint {
						break
					}
					processedData = append(processedData, d)
				}
			}

			block := NewBlock(tc.reader)
			querier := NewBlockQuerier(block)

			err = block.LoadHeaders()
			require.Nil(t, err)
			require.Equal(t, block.blooms.schema, schema)

			// Check processed data can be queried
			for i := 0; i < len(processedData); i++ {
				require.Equal(t, true, querier.Next(), "on iteration %d with error %v", i, querier.Err())
				got := querier.At()
				require.Equal(t, processedData[i].Series, got.Series)
				for _, key := range keys[i] {
					require.True(t, got.Bloom.Test(key))
				}
				require.NoError(t, querier.Err())
			}
			// ensure it's exhausted
			require.False(t, querier.Next())

			// test seek
			if !iterHasPendingData {
				i := numSeries / 2
				halfData := data[i:]
				halfKeys := keys[i:]
				require.Nil(t, querier.Seek(halfData[0].Series.Fingerprint))
				for j := 0; j < len(halfData); j++ {
					require.Equal(t, true, querier.Next(), "on iteration %d", j)
					got := querier.At()
					require.Equal(t, halfData[j].Series, got.Series)
					for _, key := range halfKeys[j] {
						require.True(t, got.Bloom.Test(key))
					}
					require.NoError(t, querier.Err())
				}
				require.False(t, querier.Next())
			}
		})
	}
}

func TestMergeBuilder(t *testing.T) {
	for _, tc := range []struct {
		desc               string
		maxBlockSize       int
		iterHasPendingData bool
	}{
		{
			desc: "no block limit",
		},
		{
			desc: "max block size",
			// Set max block big enough to fit a bunch of series
			maxBlockSize:       10 << 10,
			iterHasPendingData: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			nBlocks := 10
			numSeries := 100
			numKeysPerSeries := 100
			blocks := make([]PeekingIterator[*SeriesWithBloom], 0, nBlocks)
			data, _ := mkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 0, 0xffff, 0, 10000)
			blockOpts := BlockOptions{
				schema: Schema{
					version:  DefaultSchemaVersion,
					encoding: chunkenc.EncSnappy,
				},
				SeriesPageSize: 100,
				BloomPageSize:  10 << 10,
				BlockSize:      tc.maxBlockSize,
			}

			// Build a list of blocks containing overlapping & duplicated parts of the dataset
			for i := 0; i < nBlocks; i++ {
				// references for linking in memory reader+writer
				indexBuf := bytes.NewBuffer(nil)
				bloomsBuf := bytes.NewBuffer(nil)

				min := i * numSeries / nBlocks
				max := (i + 2) * numSeries / nBlocks // allow some overlap
				if max > len(data) {
					max = len(data)
				}

				writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
				reader := NewByteReader(indexBuf, bloomsBuf)

				builder, err := NewBlockBuilder(
					blockOpts,
					writer,
				)

				require.Nil(t, err)
				itr := NewSliceIter[SeriesWithBloom](data[min:max])
				_, _, err = builder.BuildFrom(itr)
				require.Nil(t, err)
				blocks = append(blocks, NewPeekingIter[*SeriesWithBloom](NewBlockQuerier(NewBlock(reader))))
			}

			// We're not testing the ability to extend a bloom in this test
			pop := func(_ *Series, _ *Bloom) error {
				return errors.New("not implemented")
			}

			// storage should contain references to all the series we ingested,
			// regardless of block allocation/overlap.
			storeItr := NewPeekingIter[*Series](
				NewMapIter[SeriesWithBloom, *Series](
					NewSliceIter[SeriesWithBloom](data),
					func(swb SeriesWithBloom) *Series {
						return swb.Series
					},
				),
			)

			// Ensure that the merge builder combines all the blocks correctly
			mergeBuilder := NewMergeBuilder(blocks, storeItr, pop)
			indexBuf := bytes.NewBuffer(nil)
			bloomsBuf := bytes.NewBuffer(nil)
			writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
			reader := NewByteReader(indexBuf, bloomsBuf)

			builder, err := NewBlockBuilder(
				blockOpts,
				writer,
			)
			require.Nil(t, err)

			_, _, err = mergeBuilder.Build(builder)
			require.Nil(t, err)

			firstPendingSeries, iterHasPendingData := storeItr.Peek()
			require.Equal(t, tc.iterHasPendingData, iterHasPendingData)

			processedData := data
			if iterHasPendingData {
				// If iter is not consumed, cut the data to the first pending series
				processedData = make([]SeriesWithBloom, 0, len(data))
				for _, d := range data {
					if d.Series.Fingerprint >= firstPendingSeries.Fingerprint {
						break
					}
					processedData = append(processedData, d)
				}
			}

			block := NewBlock(reader)
			querier := NewBlockQuerier(block)

			EqualIterators[*SeriesWithBloom](
				t,
				func(a, b *SeriesWithBloom) {
					require.Equal(t, a.Series, b.Series, "expected %+v, got %+v", a, b)
				},
				NewSliceIter[*SeriesWithBloom](PointerSlice(processedData)),
				querier,
			)
		})
	}
}
