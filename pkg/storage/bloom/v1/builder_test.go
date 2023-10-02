package v1

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/owen-d/BoomFilters/boom"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

type noopCloser struct {
	io.Writer
}

func (n noopCloser) Close() error {
	return nil
}

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
		bloom.sbf = *boom.NewScalableBloomFilter(1024, 0.01, 0.8)
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
	itr := NewSliceIter[SeriesWithBloom](data)

	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	builder := NewBlockBuilder(
		BlockOptions{
			schema: Schema{
				version:  DefaultSchemaVersion,
				encoding: chunkenc.EncSnappy,
			},
			SeriesPageSize: 64 << 10,
			BloomPageSize:  128 << 10,
		},
		noopCloser{indexBuf},
		noopCloser{bloomsBuf},
	)

	require.Nil(t, builder.BuildFrom(itr))
	blockReader := NewByteReader(indexBuf.Bytes(), bloomsBuf.Bytes())
	block := NewBlock(blockReader)

	seriesItr := block.Series()
	bloomItr := NewLazyBloomIter(block)

	var i int
	for seriesItr.Next() {
		require.Nil(t, seriesItr.Err())
		s := seriesItr.At()
		require.Equal(t, *data[i].Series, s.Series)

		bloom, err := bloomItr.Seek(s.Offset)
		require.Nil(t, err)
		require.Equal(t, data[i].Bloom, bloom)
		i++
	}

	require.Equal(t, false, seriesItr.Next())
	require.Equal(t, numSeries, i)
}
