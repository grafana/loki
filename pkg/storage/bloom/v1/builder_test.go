package v1

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/util/encoding"
)

var blockEncodings = []chunkenc.Encoding{
	chunkenc.EncNone,
	chunkenc.EncGZIP,
	chunkenc.EncSnappy,
	chunkenc.EncLZ4_256k,
	chunkenc.EncZstd,
}

func TestBlockOptionsRoundTrip(t *testing.T) {
	t.Parallel()
	opts := BlockOptions{
		Schema: Schema{
			version:     V1,
			encoding:    chunkenc.EncSnappy,
			nGramLength: 10,
			nGramSkip:   2,
		},
		SeriesPageSize: 100,
		BloomPageSize:  10 << 10,
		BlockSize:      10 << 20,
	}

	var enc encoding.Encbuf
	opts.Encode(&enc)

	var got BlockOptions
	err := got.DecodeFrom(bytes.NewReader(enc.Get()))
	require.Nil(t, err)

	require.Equal(t, opts, got)
}

func TestBlockBuilder_RoundTrip(t *testing.T) {
	numSeries := 100
	numKeysPerSeries := 10000
	data, keys := MkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 0, 0xffff, 0, 10000)

	for _, enc := range blockEncodings {
		// references for linking in memory reader+writer
		indexBuf := bytes.NewBuffer(nil)
		bloomsBuf := bytes.NewBuffer(nil)
		// directory for directory reader+writer
		tmpDir := t.TempDir()

		for _, tc := range []struct {
			desc               string
			writer             BlockWriter
			reader             BlockReader
			maxBlockSize       uint64
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
				// Set max block big enough to fit a bunch of series but not all of them
				maxBlockSize:       50 << 10,
				iterHasPendingData: true,
			},
		} {
			desc := fmt.Sprintf("%s/%s", tc.desc, enc)
			t.Run(desc, func(t *testing.T) {
				blockOpts := BlockOptions{
					Schema: Schema{
						version:     DefaultSchemaVersion,
						encoding:    enc,
						nGramLength: 10,
						nGramSkip:   2,
					},
					SeriesPageSize: 100,
					BloomPageSize:  10 << 10,
					BlockSize:      tc.maxBlockSize,
				}

				builder, err := NewBlockBuilder(blockOpts, tc.writer)

				require.Nil(t, err)
				itr := NewPeekingIter[SeriesWithBloom](NewSliceIter[SeriesWithBloom](data))
				_, err = builder.BuildFrom(itr)
				require.Nil(t, err)

				firstPendingSeries, iterHasPendingData := itr.Peek()
				require.Equal(t, tc.iterHasPendingData, iterHasPendingData)

				processedData := data
				if iterHasPendingData {
					lastProcessedIdx := sort.Search(len(data), func(i int) bool {
						return data[i].Series.Fingerprint >= firstPendingSeries.Series.Fingerprint
					})
					processedData = data[:lastProcessedIdx]
				}

				block := NewBlock(tc.reader, NewMetrics(nil))
				querier := NewBlockQuerier(block, &SimpleHeapAllocator{}, DefaultMaxPageSize)

				err = block.LoadHeaders()
				require.Nil(t, err)
				require.Equal(t, block.blooms.schema, blockOpts.Schema)

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
					require.NoError(t, querier.Seek(halfData[0].Series.Fingerprint))
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
}

func dedupedBlocks(blocks []PeekingIterator[*SeriesWithBloom]) Iterator[*SeriesWithBloom] {
	orderedBlocks := NewHeapIterForSeriesWithBloom(blocks...)
	return NewDedupingIter[*SeriesWithBloom](
		func(a *SeriesWithBloom, b *SeriesWithBloom) bool {
			return a.Series.Fingerprint == b.Series.Fingerprint
		},
		Identity[*SeriesWithBloom],
		func(a *SeriesWithBloom, b *SeriesWithBloom) *SeriesWithBloom {
			if len(a.Series.Chunks) > len(b.Series.Chunks) {
				return a
			}
			return b
		},
		NewPeekingIter[*SeriesWithBloom](orderedBlocks),
	)
}

func TestMergeBuilder(t *testing.T) {
	t.Parallel()

	nBlocks := 10
	numSeries := 100
	numKeysPerSeries := 100
	blocks := make([]PeekingIterator[*SeriesWithBloom], 0, nBlocks)
	data, _ := MkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 0, 0xffff, 0, 10000)
	blockOpts := BlockOptions{
		Schema: Schema{
			version:  DefaultSchemaVersion,
			encoding: chunkenc.EncSnappy,
		},
		SeriesPageSize: 100,
		BloomPageSize:  10 << 10,
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
		_, err = builder.BuildFrom(itr)
		require.Nil(t, err)
		blocks = append(blocks, NewPeekingIter[*SeriesWithBloom](NewBlockQuerier(NewBlock(reader, NewMetrics(nil)), &SimpleHeapAllocator{}, DefaultMaxPageSize)))
	}

	// We're not testing the ability to extend a bloom in this test
	pop := func(_ *Series, _ *Bloom) (int, bool, error) {
		return 0, false, errors.New("not implemented")
	}

	// storage should contain references to all the series we ingested,
	// regardless of block allocation/overlap.
	storeItr := NewMapIter[SeriesWithBloom, *Series](
		NewSliceIter[SeriesWithBloom](data),
		func(swb SeriesWithBloom) *Series {
			return swb.Series
		},
	)

	// Ensure that the merge builder combines all the blocks correctly
	mergeBuilder := NewMergeBuilder(dedupedBlocks(blocks), storeItr, pop, NewMetrics(nil))
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

	block := NewBlock(reader, NewMetrics(nil))
	querier := NewBlockQuerier(block, &SimpleHeapAllocator{}, DefaultMaxPageSize)

	EqualIterators[*SeriesWithBloom](
		t,
		func(a, b *SeriesWithBloom) {
			require.Equal(t, a.Series, b.Series, "expected %+v, got %+v", a, b)
		},
		NewSliceIter[*SeriesWithBloom](PointerSlice(data)),
		querier,
	)
}

func TestBlockReset(t *testing.T) {
	t.Parallel()
	numSeries := 100
	numKeysPerSeries := 10000
	data, _ := MkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 1, 0xffff, 0, 10000)

	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)

	schema := Schema{
		version:     DefaultSchemaVersion,
		encoding:    chunkenc.EncSnappy,
		nGramLength: 10,
		nGramSkip:   2,
	}

	builder, err := NewBlockBuilder(
		BlockOptions{
			Schema:         schema,
			SeriesPageSize: 100,
			BloomPageSize:  10 << 10,
		},
		writer,
	)

	require.Nil(t, err)
	itr := NewSliceIter[SeriesWithBloom](data)
	_, err = builder.BuildFrom(itr)
	require.Nil(t, err)
	block := NewBlock(reader, NewMetrics(nil))
	querier := NewBlockQuerier(block, &SimpleHeapAllocator{}, DefaultMaxPageSize)

	rounds := make([][]model.Fingerprint, 2)

	for i := 0; i < len(rounds); i++ {
		for querier.Next() {
			rounds[i] = append(rounds[i], querier.At().Series.Fingerprint)
		}

		err = querier.Seek(0) // reset at end
		require.Nil(t, err)
	}

	require.Equal(t, rounds[0], rounds[1])
}

// This test is a basic roundtrip test for the merge builder.
// It creates one set of blocks with the same (duplicate) data, and another set of blocks with
// disjoint data. It then merges the two sets of blocks and ensures that the merged blocks contain
// one copy of the first set (duplicate data) and one copy of the second set (disjoint data).
func TestMergeBuilder_Roundtrip(t *testing.T) {
	t.Parallel()

	numSeries := 100
	numKeysPerSeries := 100
	minTs, maxTs := model.Time(0), model.Time(10000)
	xs, _ := MkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 0, 0xffff, minTs, maxTs)

	var data [][]*SeriesWithBloom

	// First, we build the blocks

	sets := []int{
		2, // 2 blocks containint the same data
		1, // 1 block containing disjoint data
	}

	blockOpts := BlockOptions{
		Schema: Schema{
			version:     DefaultSchemaVersion,
			encoding:    chunkenc.EncSnappy, // test with different encodings?
			nGramLength: 4,                  // needs to match values from MkBasicSeriesWithBlooms
			nGramSkip:   0,                  // needs to match values from MkBasicSeriesWithBlooms
		},
		SeriesPageSize: 100,
		BloomPageSize:  10 << 10,
	}

	for i, copies := range sets {
		for j := 0; j < copies; j++ {
			// references for linking in memory reader+writer
			indexBuf := bytes.NewBuffer(nil)
			bloomsBuf := bytes.NewBuffer(nil)

			writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
			reader := NewByteReader(indexBuf, bloomsBuf)

			builder, err := NewBlockBuilder(blockOpts, writer)

			require.Nil(t, err)
			// each set of copies gets a different slice of the data
			minIdx, maxIdx := i*len(xs)/len(sets), (i+1)*len(xs)/len(sets)
			itr := NewSliceIter[SeriesWithBloom](xs[minIdx:maxIdx])
			_, err = builder.BuildFrom(itr)
			require.Nil(t, err)
			block := NewBlock(reader, NewMetrics(nil))
			querier := NewBlockQuerier(block, &SimpleHeapAllocator{}, DefaultMaxPageSize)

			// rather than use the block querier directly, collect it's data
			// so we can use it in a few places later
			var tmp []*SeriesWithBloom
			for querier.Next() {
				tmp = append(tmp, querier.At())
			}
			data = append(data, tmp)
		}
	}

	// we keep 2 copies of the data as iterators. One for the blocks, and one for the "store"
	// which will force it to reference the same series
	var blocks []PeekingIterator[*SeriesWithBloom]
	var store []PeekingIterator[*SeriesWithBloom]

	for _, x := range data {
		blocks = append(blocks, NewPeekingIter[*SeriesWithBloom](NewSliceIter[*SeriesWithBloom](x)))
		store = append(store, NewPeekingIter[*SeriesWithBloom](NewSliceIter[*SeriesWithBloom](x)))
	}

	orderedStore := NewHeapIterForSeriesWithBloom(store...)
	dedupedStore := NewDedupingIter[*SeriesWithBloom, *Series](
		func(a *SeriesWithBloom, b *Series) bool {
			return a.Series.Fingerprint == b.Fingerprint
		},
		func(swb *SeriesWithBloom) *Series {
			return swb.Series
		},
		func(a *SeriesWithBloom, b *Series) *Series {
			if len(a.Series.Chunks) > len(b.Chunks) {
				return a.Series
			}
			return b
		},
		NewPeekingIter[*SeriesWithBloom](orderedStore),
	)

	// build the new block from the old ones
	indexBuf, bloomBuf := bytes.NewBuffer(nil), bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomBuf)
	reader := NewByteReader(indexBuf, bloomBuf)
	mb := NewMergeBuilder(
		dedupedBlocks(blocks),
		dedupedStore,
		func(s *Series, b *Bloom) (int, bool, error) {
			// We're not actually indexing new data in this test
			return 0, false, nil
		},
		NewMetrics(nil),
	)
	builder, err := NewBlockBuilder(blockOpts, writer)
	require.Nil(t, err)

	checksum, _, err := mb.Build(builder)
	require.Nil(t, err)
	require.Equal(t, uint32(0xc7b4210b), checksum)

	// ensure the new block contains one copy of all the data
	// by comparing it against an iterator over the source data
	mergedBlockQuerier := NewBlockQuerier(NewBlock(reader, NewMetrics(nil)), &SimpleHeapAllocator{}, DefaultMaxPageSize)
	sourceItr := NewSliceIter[*SeriesWithBloom](PointerSlice[SeriesWithBloom](xs))

	EqualIterators[*SeriesWithBloom](
		t,
		func(a, b *SeriesWithBloom) {
			require.Equal(t, a.Series.Fingerprint, b.Series.Fingerprint)
		},
		sourceItr,
		mergedBlockQuerier,
	)
}
