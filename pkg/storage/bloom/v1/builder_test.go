package v1

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/owen-d/BoomFilters/boom"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
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
		bloom.ScalableBloomFilter = *boom.NewScalableBloomFilter(1024, 0.01, 0.8)

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
			require.Nil(t, builder.BuildFrom(itr))
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
