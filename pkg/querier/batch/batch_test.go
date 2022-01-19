package batch

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	promchunk "github.com/grafana/loki/pkg/storage/chunk/encoding"
)

func BenchmarkNewChunkMergeIterator_CreateAndIterate(b *testing.B) {
	scenarios := []struct {
		numChunks          int
		numSamplesPerChunk int
		duplicationFactor  int
		enc                promchunk.Encoding
	}{
		{numChunks: 1000, numSamplesPerChunk: 100, duplicationFactor: 1, enc: promchunk.Bigchunk},
		{numChunks: 1000, numSamplesPerChunk: 100, duplicationFactor: 3, enc: promchunk.Bigchunk},
		{numChunks: 1000, numSamplesPerChunk: 100, duplicationFactor: 1, enc: promchunk.Varbit},
		{numChunks: 1000, numSamplesPerChunk: 100, duplicationFactor: 3, enc: promchunk.Varbit},
		{numChunks: 1000, numSamplesPerChunk: 100, duplicationFactor: 1, enc: promchunk.DoubleDelta},
		{numChunks: 1000, numSamplesPerChunk: 100, duplicationFactor: 3, enc: promchunk.DoubleDelta},
		{numChunks: 1000, numSamplesPerChunk: 100, duplicationFactor: 1, enc: promchunk.PrometheusXorChunk},
		{numChunks: 1000, numSamplesPerChunk: 100, duplicationFactor: 3, enc: promchunk.PrometheusXorChunk},
		{numChunks: 100, numSamplesPerChunk: 100, duplicationFactor: 1, enc: promchunk.PrometheusXorChunk},
		{numChunks: 100, numSamplesPerChunk: 100, duplicationFactor: 3, enc: promchunk.PrometheusXorChunk},
		{numChunks: 1, numSamplesPerChunk: 100, duplicationFactor: 1, enc: promchunk.PrometheusXorChunk},
		{numChunks: 1, numSamplesPerChunk: 100, duplicationFactor: 3, enc: promchunk.PrometheusXorChunk},
	}

	for _, scenario := range scenarios {
		name := fmt.Sprintf("chunks: %d samples per chunk: %d duplication factor: %d encoding: %s",
			scenario.numChunks,
			scenario.numSamplesPerChunk,
			scenario.duplicationFactor,
			scenario.enc.String())

		chunks := createChunks(b, scenario.numChunks, scenario.numSamplesPerChunk, scenario.duplicationFactor, scenario.enc)

		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()

			for n := 0; n < b.N; n++ {
				it := NewChunkMergeIterator(chunks, 0, 0)
				for it.Next() {
					it.At()
				}

				// Ensure no error occurred.
				if it.Err() != nil {
					b.Fatal(it.Err().Error())
				}
			}
		})
	}
}

func TestSeekCorrectlyDealWithSinglePointChunks(t *testing.T) {
	chunkOne := mkChunk(t, model.Time(1*step/time.Millisecond), 1, promchunk.PrometheusXorChunk)
	chunkTwo := mkChunk(t, model.Time(10*step/time.Millisecond), 1, promchunk.PrometheusXorChunk)
	chunks := []chunk.Chunk{chunkOne, chunkTwo}

	sut := NewChunkMergeIterator(chunks, 0, 0)

	// Following calls mimics Prometheus's query engine behaviour for VectorSelector.
	require.True(t, sut.Next())
	require.True(t, sut.Seek(0))

	actual, val := sut.At()
	require.Equal(t, float64(1*time.Second/time.Millisecond), val) // since mkChunk use ts as value.
	require.Equal(t, int64(1*time.Second/time.Millisecond), actual)
}

func createChunks(b *testing.B, numChunks, numSamplesPerChunk, duplicationFactor int, enc promchunk.Encoding) []chunk.Chunk {
	result := make([]chunk.Chunk, 0, numChunks)

	for d := 0; d < duplicationFactor; d++ {
		for c := 0; c < numChunks; c++ {
			minTime := step * time.Duration(c*numSamplesPerChunk)
			result = append(result, mkChunk(b, model.Time(minTime.Milliseconds()), numSamplesPerChunk, enc))
		}
	}

	return result
}
