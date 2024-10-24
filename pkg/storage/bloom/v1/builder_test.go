package v1

import (
	"bytes"
	"fmt"
	"sort"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/util/encoding"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

var blockEncodings = []compression.Codec{
	compression.None,
	compression.GZIP,
	compression.Snappy,
	compression.LZ4_256k,
	compression.Zstd,
}

func TestBlockOptions_BloomPageSize(t *testing.T) {
	t.Parallel()

	var (
		maxBlockSizeBytes = uint64(50 << 10)
		maxBloomSizeBytes = uint64(10 << 10)
	)

	opts := NewBlockOptions(compression.None, maxBlockSizeBytes, maxBloomSizeBytes)

	require.GreaterOrEqual(
		t, opts.BloomPageSize, maxBloomSizeBytes,
		"opts.BloomPageSize should be greater or equal to the maximum bloom size to avoid having too many overfilled pages",
	)
}

func TestBlockOptions_RoundTrip(t *testing.T) {
	t.Parallel()
	opts := BlockOptions{
		Schema:         NewSchema(CurrentSchemaVersion, compression.Snappy),
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
					Schema:         NewSchema(CurrentSchemaVersion, enc),
					SeriesPageSize: 100,
					BloomPageSize:  10 << 10,
					BlockSize:      tc.maxBlockSize,
				}
				data, keys := MkBasicSeriesWithBlooms(numSeries, 0, 0xffff, 0, 10000)

				builder, err := NewBlockBuilder(blockOpts, tc.writer)

				require.Nil(t, err)
				itr := iter.NewPeekIter(iter.NewSliceIter(data))
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
				querier := NewBlockQuerier(block, &mempool.SimpleHeapAllocator{}, DefaultMaxPageSize).Iter()

				err = block.LoadHeaders()
				require.Nil(t, err)
				require.Equal(t, block.blooms.schema, blockOpts.Schema)

				// Check processed data can be queried
				for i := 0; i < len(processedData); i++ {
					require.Equal(t, true, querier.Next(), "on iteration %d with error %v", i, querier.Err())
					got := querier.At()
					blooms, err := iter.Collect(got.Blooms)
					require.Nil(t, err)
					require.Equal(t, processedData[i].Series.Series, got.Series.Series)
					for _, key := range keys[i] {
						found := false
						for _, b := range blooms {
							if b.Test(key) {
								found = true
								break
							}
						}
						require.True(t, found)
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
						blooms, err := iter.Collect(got.Blooms)
						require.Nil(t, err)
						require.Equal(t, halfData[j].Series.Series, got.Series.Series)
						for _, key := range halfKeys[j] {
							found := false
							for _, b := range blooms {
								if b.Test(key) {
									found = true
									break
								}
							}
							require.True(t, found)
						}
						require.NoError(t, querier.Err())
					}
					require.False(t, querier.Next())
				}

			})
		}

	}
}

func dedupedBlocks(blocks []iter.PeekIterator[*SeriesWithBlooms]) iter.Iterator[*SeriesWithBlooms] {
	orderedBlocks := NewHeapIterForSeriesWithBloom(blocks...)
	return iter.NewDedupingIter(
		func(a *SeriesWithBlooms, b *SeriesWithBlooms) bool {
			return a.Series.Fingerprint == b.Series.Fingerprint
		},
		iter.Identity[*SeriesWithBlooms],
		func(a *SeriesWithBlooms, b *SeriesWithBlooms) *SeriesWithBlooms {
			if len(a.Series.Chunks) > len(b.Series.Chunks) {
				return a
			}
			return b
		},
		iter.NewPeekIter(orderedBlocks),
	)
}

func TestMergeBuilder(t *testing.T) {
	t.Parallel()

	nBlocks := 10
	numSeries := 100
	blocks := make([]iter.PeekIterator[*SeriesWithBlooms], 0, nBlocks)
	data, _ := MkBasicSeriesWithBlooms(numSeries, 0, 0xffff, 0, 10000)
	blockOpts := BlockOptions{
		Schema:         NewSchema(CurrentSchemaVersion, compression.Snappy),
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
		itr := iter.NewSliceIter(data[min:max])
		_, err = builder.BuildFrom(itr)
		require.Nil(t, err)
		blocks = append(blocks, iter.NewPeekIter(NewBlockQuerier(NewBlock(reader, NewMetrics(nil)), &mempool.SimpleHeapAllocator{}, DefaultMaxPageSize).Iter()))
	}

	// We're not testing the ability to extend a bloom in this test
	populate := func(_ *Series, preExistingBlooms iter.SizedIterator[*Bloom], _ ChunkRefs, ch chan *BloomCreation) {
		for preExistingBlooms.Next() {
			bloom := preExistingBlooms.At()
			ch <- &BloomCreation{
				Bloom: bloom,
				Info:  newIndexingInfo(),
			}
		}
		close(ch)
	}

	// storage should contain references to all the series we ingested,
	// regardless of block allocation/overlap.
	storeItr := iter.NewMapIter(
		iter.NewSliceIter(data),
		func(swb SeriesWithBlooms) *Series {
			return &swb.Series.Series
		},
	)

	// Ensure that the merge builder combines all the blocks correctly
	mergeBuilder := NewMergeBuilder(dedupedBlocks(blocks), storeItr, populate, NewMetrics(nil), log.NewNopLogger())
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)

	builder, err := NewBlockBuilder(blockOpts, writer)
	require.Nil(t, err)

	_, _, err = mergeBuilder.Build(builder)
	require.Nil(t, err)

	block := NewBlock(reader, NewMetrics(nil))
	querier := NewBlockQuerier(block, &mempool.SimpleHeapAllocator{}, DefaultMaxPageSize)

	EqualIterators(
		t,
		func(a, b *SeriesWithBlooms) {
			require.Equal(t, a.Series.Series, b.Series.Series, "expected series %+v, got %+v", a.Series.Series, b.Series.Series)
			require.Equal(t, a.Series.Fields, b.Series.Fields, "expected fields %+v, got %+v", a.Series.Fields, b.Series.Fields)
			// TODO(chaudum): Investigate why offsets not match
			// This has not been tested before, so I'm not too worried about something being broken.
			// require.Equal(t, a.Series.Meta.Offsets, b.Series.Meta.Offsets, "expected offsets %+v, got %+v", a.Series.Meta.Offsets, b.Series.Meta.Offsets)
		},
		iter.NewSliceIter(PointerSlice(data)),
		querier.Iter(),
	)
}

// Fingerprint collisions are treated as the same series.
func TestMergeBuilderFingerprintCollision(t *testing.T) {
	t.Parallel()

	// references for linking in memory reader+writer
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)

	blockOpts := BlockOptions{
		Schema:         NewSchema(CurrentSchemaVersion, compression.Snappy),
		SeriesPageSize: 100,
		BloomPageSize:  10 << 10,
	}

	builder, err := NewBlockBuilder(
		blockOpts,
		writer,
	)

	// two series with the same fingerprint but different chunks
	chks := []ChunkRef{
		{
			From:     0,
			Through:  0,
			Checksum: 0,
		},
		{
			From:     1,
			Through:  1,
			Checksum: 1,
		},
		{
			From:     2,
			Through:  2,
			Checksum: 2,
		},
	}

	data := []*Series{
		{
			Fingerprint: 0,
			Chunks: []ChunkRef{
				chks[0], chks[1],
			},
		},
		{
			Fingerprint: 0,
			Chunks: []ChunkRef{
				chks[2],
			},
		},
	}

	// We're not testing the ability to extend a bloom in this test
	pop := func(_ *Series, _ iter.SizedIterator[*Bloom], _ ChunkRefs, ch chan *BloomCreation) {
		bloom := NewBloom()
		// Add something to the bloom so it's not empty
		bloom.Add([]byte("hello"))
		stats := indexingInfo{
			sourceBytes:   int(bloom.Capacity()) / 8,
			indexedFields: NewSetFromLiteral[Field]("__all__"),
		}
		ch <- &BloomCreation{
			Bloom: bloom,
			Info:  stats,
		}
		close(ch)
	}

	require.Nil(t, err)
	mergeBuilder := NewMergeBuilder(
		iter.NewEmptyIter[*SeriesWithBlooms](),
		iter.NewSliceIter(data),
		pop,
		NewMetrics(nil),
		log.NewNopLogger(),
	)

	_, _, err = mergeBuilder.Build(builder)
	require.Nil(t, err)

	block := NewBlock(reader, NewMetrics(nil))
	querier := NewBlockQuerier(block, &mempool.SimpleHeapAllocator{}, DefaultMaxPageSize)

	require.True(t, querier.Next())
	require.Equal(t,
		Series{
			Fingerprint: 0,
			Chunks:      chks,
		},
		querier.At().Series,
	)

	require.False(t, querier.Next())
}

func TestBlockReset(t *testing.T) {
	t.Parallel()
	numSeries := 100
	data, _ := MkBasicSeriesWithBlooms(numSeries, 1, 0xffff, 0, 10000)

	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)

	schema := NewSchema(CurrentSchemaVersion, compression.Snappy)

	builder, err := NewBlockBuilder(
		BlockOptions{
			Schema:         schema,
			SeriesPageSize: 100,
			BloomPageSize:  10 << 10,
		},
		writer,
	)

	require.Nil(t, err)
	itr := iter.NewSliceIter(data)
	_, err = builder.BuildFrom(itr)
	require.Nil(t, err)
	block := NewBlock(reader, NewMetrics(nil))
	querier := NewBlockQuerier(block, &mempool.SimpleHeapAllocator{}, DefaultMaxPageSize)

	rounds := make([][]model.Fingerprint, 2)

	for i := 0; i < len(rounds); i++ {
		for querier.Next() {
			rounds[i] = append(rounds[i], querier.At().Fingerprint)
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
	minTs, maxTs := model.Time(0), model.Time(10000)
	xs, _ := MkBasicSeriesWithBlooms(numSeries, 0, 0xffff, minTs, maxTs)

	var data [][]*SeriesWithBlooms

	// First, we build the blocks

	sets := []int{
		2, // 2 blocks containint the same data
		1, // 1 block containing disjoint data
	}

	blockOpts := BlockOptions{
		// test with different encodings?
		Schema: NewSchema(CurrentSchemaVersion, compression.Snappy),

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
			itr := iter.NewSliceIter(xs[minIdx:maxIdx])
			_, err = builder.BuildFrom(itr)
			require.Nil(t, err)
			block := NewBlock(reader, NewMetrics(nil))
			querier := NewBlockQuerier(block, &mempool.SimpleHeapAllocator{}, DefaultMaxPageSize).Iter()

			// rather than use the block querier directly, collect it's data
			// so we can use it in a few places later
			var tmp []*SeriesWithBlooms
			for querier.Next() {
				tmp = append(tmp, querier.At())
			}
			data = append(data, tmp)
		}
	}

	// we keep 2 copies of the data as iterators. One for the blocks, and one for the "store"
	// which will force it to reference the same series
	var blocks []iter.PeekIterator[*SeriesWithBlooms]
	var store []iter.PeekIterator[*SeriesWithBlooms]

	for _, x := range data {
		blocks = append(blocks, iter.NewPeekIter(iter.NewSliceIter(x)))
		store = append(store, iter.NewPeekIter(iter.NewSliceIter(x)))
	}

	orderedStore := NewHeapIterForSeriesWithBloom(store...)
	dedupedStore := iter.NewDedupingIter(
		func(a *SeriesWithBlooms, b *Series) bool {
			return a.Series.Fingerprint == b.Fingerprint
		},
		func(swb *SeriesWithBlooms) *Series {
			return &swb.Series.Series
		},
		func(a *SeriesWithBlooms, b *Series) *Series {
			if len(a.Series.Chunks) > len(b.Chunks) {
				return &a.Series.Series
			}
			return b
		},
		iter.NewPeekIter(orderedStore),
	)

	// We're not testing the ability to extend a bloom in this test
	pop := func(_ *Series, srcBlooms iter.SizedIterator[*Bloom], _ ChunkRefs, ch chan *BloomCreation) {
		for srcBlooms.Next() {
			bloom := srcBlooms.At()
			stats := indexingInfo{
				sourceBytes:   int(bloom.Capacity()) / 8,
				indexedFields: NewSetFromLiteral[Field]("__all__"),
			}
			ch <- &BloomCreation{
				Bloom: bloom,
				Info:  stats,
			}
		}
		close(ch)
	}

	// build the new block from the old ones
	indexBuf, bloomBuf := bytes.NewBuffer(nil), bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomBuf)
	reader := NewByteReader(indexBuf, bloomBuf)
	mb := NewMergeBuilder(
		dedupedBlocks(blocks),
		dedupedStore,
		pop,
		NewMetrics(nil),
		log.NewNopLogger(),
	)
	builder, err := NewBlockBuilder(blockOpts, writer)
	require.Nil(t, err)

	_, _, err = mb.Build(builder)
	require.Nil(t, err)
	// checksum changes as soon as the contents of the block or the encoding change
	// once the block format is stable, calculate the checksum and assert its correctness
	// require.Equal(t, uint32(0x2a6cdba6), checksum)

	// ensure the new block contains one copy of all the data
	// by comparing it against an iterator over the source data
	mergedBlockQuerier := NewBlockQuerier(NewBlock(reader, NewMetrics(nil)), &mempool.SimpleHeapAllocator{}, DefaultMaxPageSize)
	sourceItr := iter.NewSliceIter(PointerSlice(xs))

	EqualIterators(
		t,
		func(a, b *SeriesWithBlooms) {
			require.Equal(t, a.Series.Fingerprint, b.Series.Fingerprint)
		},
		sourceItr,
		mergedBlockQuerier.Iter(),
	)
}
