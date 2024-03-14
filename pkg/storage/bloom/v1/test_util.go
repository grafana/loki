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

func MakeBlock(t testing.TB, nth int, fromFp, throughFp model.Fingerprint, fromTs, throughTs model.Time) (*Block, []SeriesWithBloom, [][][]byte) {
	// references for linking in memory reader+writer
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)
	numSeries := int(throughFp-fromFp) / nth
	data, keys := MkBasicSeriesWithBlooms(numSeries, nth, fromFp, throughFp, fromTs, throughTs)

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
	return block, data, keys
}

func MkBasicSeriesWithBlooms(nSeries, _ int, fromFp, throughFp model.Fingerprint, fromTs, throughTs model.Time) (seriesList []SeriesWithBloom, keysList [][][]byte) {
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
				From:     from,
				Through:  from.Add(timeDelta),
				Checksum: uint32(i),
			},
		}

		var bloom Bloom
		bloom.ScalableBloomFilter = *filter.NewScalableBloomFilter(1024, 0.01, 0.8)

		keys := make([][]byte, 0, int(step))
		for _, chk := range series.Chunks {
			tokenBuf, prefixLen := prefixedToken(nGramLen, chk, nil)

			for j := 0; j < int(step); j++ {
				line := fmt.Sprintf("%04x:%04x", int(series.Fingerprint), j)
				it := tokenizer.Tokens(line)
				for it.Next() {
					key := it.At()
					// series-level key
					bloom.Add(key)

					// chunk-level key
					tokenBuf = append(tokenBuf[:prefixLen], key...)
					bloom.Add(tokenBuf)

					keyCopy := key
					keys = append(keys, keyCopy)
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
