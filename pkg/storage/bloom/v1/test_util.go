package v1

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func MakeBlockQuerier(t testing.TB, fromFp, throughFp uint64, fromTs, throughTs int64) *BlockQuerier {
	// references for linking in memory reader+writer
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)
	numSeries := int(throughFp - fromFp)
	numKeysPerSeries := 1000
	data, _ := mkBasicSeriesWithBlooms(
		numSeries,
		numKeysPerSeries,
		model.Fingerprint(fromFp),
		model.Fingerprint(throughFp),
		model.TimeFromUnix(fromTs),
		model.TimeFromUnix(throughTs),
	)

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
	return NewBlockQuerier(block)
}

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
