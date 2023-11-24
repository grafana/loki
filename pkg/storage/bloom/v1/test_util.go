package v1

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
)

func MakeBlockQuerier(t testing.TB, fromFp, throughFp model.Fingerprint, fromTs, throughTs model.Time) (*BlockQuerier, []SeriesWithBloom) {
	// references for linking in memory reader+writer
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)
	numSeries := int(throughFp - fromFp)
	numKeysPerSeries := 1000
	data, _ := mkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, fromFp, throughFp, fromTs, throughTs)

	builder, err := NewBlockBuilder(
		BlockOptions{
			schema: Schema{
				version:  DefaultSchemaVersion,
				encoding: chunkenc.EncSnappy,
			},
			SeriesPageSize: 100,
			BloomPageSize:  10 << 10,
		},
		writer,
	)
	require.Nil(t, err)
	itr := NewSliceIter[SeriesWithBloom](data)
	_, err = builder.BuildFrom(itr)
	require.Nil(t, err)
	block := NewBlock(reader)
	return NewBlockQuerier(block), data
}

func mkBasicSeriesWithBlooms(nSeries, keysPerSeries int, fromFp, throughFp model.Fingerprint, fromTs, throughTs model.Time) (seriesList []SeriesWithBloom, keysList [][][]byte) {
	seriesList = make([]SeriesWithBloom, 0, nSeries)
	keysList = make([][][]byte, 0, nSeries)

	step := (throughFp - fromFp) / model.Fingerprint(nSeries)
	timeDelta := time.Duration(throughTs.Sub(fromTs).Nanoseconds() / int64(nSeries))

	for i := 0; i < nSeries; i++ {
		var series Series
		series.Fingerprint = fromFp + model.Fingerprint(i)*step
		from := fromTs.Add(timeDelta * time.Duration(i))
		series.Chunks = []ChunkRef{
			{
				Start:    from,
				End:      from.Add(timeDelta),
				Checksum: uint32(i),
			},
		}

		var bloom Bloom
		bloom.ScalableBloomFilter = *filter.NewScalableBloomFilter(1024, 0.01, 0.8)

		keys := make([][]byte, 0, keysPerSeries)
		for j := 0; j < keysPerSeries; j++ {
			key := []byte(fmt.Sprint(i*keysPerSeries + j))
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
