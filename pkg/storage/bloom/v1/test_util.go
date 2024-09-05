package v1

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/storage/bloom/v1/filter"
)

// TODO(owen-d): this should probably be in it's own testing-util package

func MakeBlock(t testing.TB, nth int, fromFp, throughFp model.Fingerprint, fromTs, throughTs model.Time) (*Block, []SeriesWithBlooms, [][][]byte) {
	// references for linking in memory reader+writer
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	writer := NewMemoryBlockWriter(indexBuf, bloomsBuf)
	reader := NewByteReader(indexBuf, bloomsBuf)
	numSeries := int(throughFp-fromFp) / nth
	data, keys := MkBasicSeriesWithBlooms(numSeries, fromFp, throughFp, fromTs, throughTs)

	builder, err := NewBlockBuilder(
		BlockOptions{
			Schema: Schema{
				version:     CurrentSchemaVersion,
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
	itr := iter.NewSliceIter[SeriesWithBlooms](data)
	_, err = builder.BuildFrom(itr)
	require.Nil(t, err)
	block := NewBlock(reader, NewMetrics(nil))
	return block, data, keys
}

// This is a helper type used in tests that buffers blooms and can be turned into
// the commonly used iterator form *SeriesWithBlooms.
type SeriesWithLiteralBlooms struct {
	Series *Series
	Blooms []*Bloom
}

func (s *SeriesWithLiteralBlooms) SeriesWithBlooms() SeriesWithBlooms {
	return SeriesWithBlooms{
		Series: s.Series,
		Blooms: iter.NewSliceIter[*Bloom](s.Blooms),
	}
}

func MkBasicSeriesWithBlooms(nSeries int, fromFp, throughFp model.Fingerprint, fromTs, throughTs model.Time) (seriesList []SeriesWithBlooms, keysList [][][]byte) {
	series, keys := MkBasicSeriesWithLiteralBlooms(nSeries, fromFp, throughFp, fromTs, throughTs)
	mapped := make([]SeriesWithBlooms, 0, len(series))
	for _, s := range series {
		mapped = append(mapped, s.SeriesWithBlooms())
	}

	return mapped, keys
}

func MkBasicSeriesWithLiteralBlooms(nSeries int, fromFp, throughFp model.Fingerprint, fromTs, throughTs model.Time) (seriesList []SeriesWithLiteralBlooms, keysList [][][]byte) {
	const nGramLen = 4
	seriesList = make([]SeriesWithLiteralBlooms, 0, nSeries)
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

		seriesList = append(seriesList, SeriesWithLiteralBlooms{
			Series: &series,
			Blooms: []*Bloom{&bloom},
		})
		keysList = append(keysList, keys)
	}
	return
}

func EqualIterators[T any](t *testing.T, test func(a, b T), expected, actual iter.Iterator[T]) {
	for expected.Next() {
		require.True(t, actual.Next())
		a, b := expected.At(), actual.At()
		test(a, b)
	}
	require.False(t, actual.Next())
	require.Nil(t, expected.Err())
	require.Nil(t, actual.Err())
}

// CompareIterators is a testing utility for comparing iterators of different types.
// It accepts a callback which can be used to assert characteristics of the corersponding elements
// of the two iterators.
// It also ensures that the lengths are the same and there are no errors from either iterator.
func CompareIterators[A, B any](
	t *testing.T,
	f func(t *testing.T, a A, b B),
	a iter.Iterator[A],
	b iter.Iterator[B],
) {
	for a.Next() {
		require.True(t, b.Next())
		f(t, a.At(), b.At())
	}
	require.False(t, b.Next())
	require.NoError(t, a.Err())
	require.NoError(t, b.Err())
}
