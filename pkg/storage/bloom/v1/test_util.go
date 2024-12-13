package v1

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"

	"github.com/grafana/loki/pkg/push"
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
			Schema:         NewSchema(CurrentSchemaVersion, compression.Snappy),
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
	return block, data, keys
}

func newSeriesWithBlooms(series Series, blooms []*Bloom) SeriesWithBlooms {
	offsets := make([]BloomOffset, 0, len(blooms))
	for i := range blooms {
		offsets = append(offsets, BloomOffset{Page: i, ByteOffset: 0})
	}
	return SeriesWithBlooms{
		Series: &SeriesWithMeta{
			Series: series,
			Meta: Meta{
				Fields:  NewSetFromLiteral[Field]("trace_id"),
				Offsets: offsets,
			},
		},
		Blooms: iter.NewSliceIter(blooms),
	}
}

func MkBasicSeriesWithBlooms(nSeries int, fromFp, throughFp model.Fingerprint, fromTs, throughTs model.Time) ([]SeriesWithBlooms, [][][]byte) {
	// return values
	seriesList := make([]SeriesWithBlooms, 0, nSeries)
	keysList := make([][][]byte, 0, nSeries)

	numChunksPerSeries := 10
	numBloomsPerSeries := 2

	step := (throughFp - fromFp) / model.Fingerprint(nSeries)
	timeDelta := time.Duration(throughTs.Sub(fromTs).Nanoseconds() / int64(numChunksPerSeries))

	for i := 0; i < nSeries; i++ {
		var series Series
		var blooms []*Bloom

		series.Fingerprint = fromFp + model.Fingerprint(i)*step
		for from := fromTs; from < throughTs; from = from.Add(timeDelta) {
			series.Chunks = append(series.Chunks,
				ChunkRef{
					From:    from,
					Through: from.Add(timeDelta),
				},
			)
		}

		keys := make([][]byte, 0, int(step))

		chunkBatchSize := (series.Chunks.Len() + numBloomsPerSeries - 1) / numBloomsPerSeries
		for j := 0; j < numBloomsPerSeries; j++ {
			bloom := NewBloom()

			batchStart, batchEnd := j*chunkBatchSize, min(series.Chunks.Len(), (j+1)*chunkBatchSize)
			for x, chk := range series.Chunks[batchStart:batchEnd] {
				tokenizer := NewStructuredMetadataTokenizer(string(prefixForChunkRef(chk)))
				kv := push.LabelAdapter{Name: "trace_id", Value: fmt.Sprintf("%s:%04x", series.Fingerprint, j*chunkBatchSize+x)}
				it := tokenizer.Tokens(kv)
				for it.Next() {
					key := []byte(it.At())
					bloom.Add(key)
					keys = append(keys, key)
				}
			}
			blooms = append(blooms, bloom)
		}

		seriesList = append(seriesList, newSeriesWithBlooms(series, blooms))
		keysList = append(keysList, keys)
	}

	return seriesList, keysList
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
	var i int
	for a.Next() {
		require.Truef(t, b.Next(), "'a' has %dth element but 'b' does not'", i)
		f(t, a.At(), b.At())
		i++
	}
	require.False(t, b.Next())
	require.NoError(t, a.Err())
	require.NoError(t, b.Err())
}
