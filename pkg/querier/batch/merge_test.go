package batch

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage/chunk/encoding"
)

func TestMergeIter(t *testing.T) {
	forEncodings(t, func(t *testing.T, enc encoding.Encoding) {
		chunk1 := mkGenericChunk(t, 0, 100, enc)
		chunk2 := mkGenericChunk(t, model.TimeFromUnix(25), 100, enc)
		chunk3 := mkGenericChunk(t, model.TimeFromUnix(50), 100, enc)
		chunk4 := mkGenericChunk(t, model.TimeFromUnix(75), 100, enc)
		chunk5 := mkGenericChunk(t, model.TimeFromUnix(100), 100, enc)

		iter := newMergeIterator([]GenericChunk{chunk1, chunk2, chunk3, chunk4, chunk5})
		testIter(t, 200, newIteratorAdapter(iter))

		iter = newMergeIterator([]GenericChunk{chunk1, chunk2, chunk3, chunk4, chunk5})
		testSeek(t, 200, newIteratorAdapter(iter))
	})
}

func TestMergeHarder(t *testing.T) {
	forEncodings(t, func(t *testing.T, enc encoding.Encoding) {
		var (
			numChunks = 24 * 15
			chunks    = make([]GenericChunk, 0, numChunks)
			from      = model.Time(0)
			offset    = 30
			samples   = 100
		)
		for i := 0; i < numChunks; i++ {
			chunks = append(chunks, mkGenericChunk(t, from, samples, enc))
			from = from.Add(time.Duration(offset) * time.Second)
		}
		iter := newMergeIterator(chunks)
		testIter(t, offset*numChunks+samples-offset, newIteratorAdapter(iter))

		iter = newMergeIterator(chunks)
		testSeek(t, offset*numChunks+samples-offset, newIteratorAdapter(iter))
	})
}
