package v1

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
)

func mkBasicSeriesWithBlooms(n int, fromFp, throughFp model.Fingerprint, fromTs, throughTs model.Time) (seriesList []SeriesWithBloom) {
	for i := 0; i < n; i++ {
		var series Series
		step := (throughFp - fromFp) / (model.Fingerprint(n))
		series.Fingerprint = fromFp + model.Fingerprint(i)*step
		timeDelta := fromTs + (throughTs-fromTs)/model.Time(n)*model.Time(i)
		series.Chunks = []ChunkRef{
			{
				Start:    fromTs + timeDelta*model.Time(i),
				End:      fromTs + timeDelta*model.Time(i),
				Checksum: uint32(i),
			},
		}

		var bloom Bloom
		bloom.sbf = *filter.NewScalableBloomFilter(1024, 0.01, 0.8)
		bloom.sbf.Add([]byte(fmt.Sprint(i)))

		seriesList = append(seriesList, SeriesWithBloom{
			Series: &series,
			Bloom:  &bloom,
		})
	}
	return
}

func TestBlockBuilderRoundTrip(t *testing.T) {
	numSeries := 100
	data := mkBasicSeriesWithBlooms(numSeries, 0, 0xffff, 0, 10000)

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
				require.Equal(t, data[i].Bloom, got.Bloom)
			}
			// ensure no error
			require.Nil(t, querier.Err())
			// ensure it's exhausted
			require.Equal(t, false, querier.Next())

			// test seek
			i := numSeries / 2
			half := data[i:]
			require.Nil(t, querier.Seek(half[0].Series.Fingerprint))
			for j := 0; j < len(half); j++ {
				require.Equal(t, true, querier.Next(), "on iteration %d", j)
				got := querier.At()
				require.Equal(t, half[j].Series, got.Series)
				require.Equal(t, half[j].Bloom, got.Bloom)
				require.Nil(t, querier.Err())
			}
			require.Equal(t, false, querier.Next())

		})
	}
}
