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

// TODO(owen-d): this should probably be in it's own testing-util package

func MakeBlockQuerier(t testing.TB, fromFp, throughFp model.Fingerprint, fromTs, throughTs model.Time) (*BlockQuerier, []SeriesWithBloom) {
	// references for linking in memory reader+writer
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)
	numSeries := int(throughFp - fromFp)
	numKeysPerSeries := 1000
	data, _ := MkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, fromFp, throughFp, fromTs, throughTs)

	builder, err := NewBlockBuilder(
		BlockOptions{
			Schema: Schema{
				version:     DefaultSchemaVersion,
				encoding:    chunkenc.EncSnappy,
				nGramLength: 4, // see DefaultNGramLength in bloom_tokenizer_test.go
				nGramSkip:   0, // see DefaultNGramSkip in bloom_tokenizer_test.go
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

func MkBasicSeriesWithBlooms(nSeries, keysPerSeries int, fromFp, throughFp model.Fingerprint, fromTs, throughTs model.Time) (seriesList []SeriesWithBloom, keysList [][][]byte) {
	const nGramLen = 4
	seriesList = make([]SeriesWithBloom, 0, nSeries)
	keysList = make([][][]byte, 0, nSeries)

	step := (throughFp - fromFp) / model.Fingerprint(nSeries)
	timeDelta := time.Duration(throughTs.Sub(fromTs).Nanoseconds() / int64(nSeries))

	tokenizer := NewNGramTokenizer(nGramLen, 0)
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
			it := tokenizer.Tokens(fmt.Sprintf("series %d", i*keysPerSeries+j))
			for it.Next() {
				key := it.At()
				// series-level key
				bloom.Add(key)
				keys = append(keys, key)

				// chunk-level key
				for _, chk := range series.Chunks {
					tokenBuf, prefixLen := prefixedToken(nGramLen, chk, nil)
					tokenBuf = append(tokenBuf[:prefixLen], key...)
					bloom.Add(tokenBuf)
				}
			}
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
