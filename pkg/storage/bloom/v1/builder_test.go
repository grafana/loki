package v1

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
)

func mkBasicSeriesWithBlooms(nSeries, keysPerSeries int, fromFp, throughFp model.Fingerprint, fromTs, throughTs model.Time) (seriesList []SeriesWithBloom, keysList [][][]byte) {
	seriesList = make([]SeriesWithBloom, 0, nSeries)
	keysList = make([][][]byte, 0, nSeries)
	for i := 0; i < nSeries; i++ {
		var series Series
		step := (throughFp - fromFp) / (model.Fingerprint(nSeries))
		series.Fingerprint = fromFp + model.Fingerprint(i)*step
		timeDelta := fromTs + (throughTs-fromTs)/model.Time(nSeries)*model.Time(i)
		series.Chunks = []ChunkRef{
			{
				Start:    fromTs + timeDelta*model.Time(i),
				End:      fromTs + timeDelta*model.Time(i),
				Checksum: uint32(i),
			},
		}

		var bloom Bloom
		bloom.ScalableBloomFilter = *filter.NewScalableBloomFilter(1024, 0.01, 0.8)

		keys := make([][]byte, 0, keysPerSeries)
		for j := 0; j < keysPerSeries; j++ {
			key := []byte(fmt.Sprint(j))
			bloom.Add(key)
			keys = append(keys, key)
		}

		seriesList = append(seriesList, SeriesWithBloom{
			Series: &series,
			Bloom:  &bloom,
		})
		keysList = append(keysList, keys)
	}
	return
}

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
		desc   string
		writer BlockWriter
		reader BlockReader
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
	} {
		t.Run(tc.desc, func(t *testing.T) {
			builder, err := NewBlockBuilder(
				BlockOptions{
					schema: Schema{
						version:  DefaultSchemaVersion,
						encoding: chunkenc.EncSnappy,
					},
					SeriesPageSize: 100,
					BloomPageSize:  10 << 10,
				},
				tc.writer,
			)

			require.Nil(t, err)
			itr := NewSliceIter[SeriesWithBloom](data)
			_, err = builder.BuildFrom(itr)
			require.Nil(t, err)
			block := NewBlock(tc.reader)
			querier := NewBlockQuerier(block)

			for i := 0; i < len(data); i++ {
				require.Equal(t, true, querier.Next(), "on iteration %d with error %v", i, querier.Err())
				got := querier.At()
				require.Equal(t, data[i].Series, got.Series)
				for _, key := range keys[i] {
					require.True(t, got.Bloom.Test(key))
				}
				require.NoError(t, querier.Err())
			}
			// ensure it's exhausted
			require.False(t, querier.Next())

			// test seek
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

		})
	}
}

func TestMergeBuilder(t *testing.T) {

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
		blocks = append(blocks, NewPeekingIter[*SeriesWithBloom](NewBlockQuerier(NewBlock(reader))))
	}

	// We're not testing the ability to extend a bloom in this test
	pop := func(_ *Series, _ *Bloom) error {
		return errors.New("not implemented")
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

	require.Nil(t, mergeBuilder.Build(builder))
	block := NewBlock(reader)
	querier := NewBlockQuerier(block)

	EqualIterators[*SeriesWithBloom](
		t,
		func(a, b *SeriesWithBloom) {
			require.Equal(t, a.Series, b.Series, "expected %+v, got %+v", a, b)
		},
		NewSliceIter[*SeriesWithBloom](PointerSlice(data)),
		querier,
	)
}
