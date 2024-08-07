package wal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/wal"
	walchunks "github.com/grafana/loki/v3/pkg/storage/wal/chunks"
)

type mockBlockStorage struct {
	data map[string][]byte
}

func (m *mockBlockStorage) GetObjectRange(_ context.Context, objectKey string, off, length int64) (io.ReadCloser, error) {
	data := m.data[objectKey]
	return io.NopCloser(bytes.NewReader(data[off : off+length])), nil
}

func TestChunksEntryIterator(t *testing.T) {
	ctx := context.Background()
	storage := &mockBlockStorage{data: make(map[string][]byte)}

	// Generate test data with multiple batches
	chunkData := generateTestChunkData(5 * defaultBatchSize)
	chks := writeChunksToStorage(t, storage, chunkData)

	tests := []struct {
		name      string
		direction logproto.Direction
		start     time.Time
		end       time.Time
		expected  []logproto.Entry
	}{
		{
			name:      "forward direction, all entries",
			direction: logproto.FORWARD,
			start:     time.Unix(0, 0),
			end:       time.Unix(int64(5*defaultBatchSize+1), 0),
			expected:  flattenEntries(chunkData),
		},
		{
			name:      "backward direction, all entries",
			direction: logproto.BACKWARD,
			start:     time.Unix(0, 0),
			end:       time.Unix(int64(5*defaultBatchSize+1), 0),
			expected:  reverseEntries(flattenEntries(chunkData)),
		},
		{
			name:      "forward direction, partial range",
			direction: logproto.FORWARD,
			start:     time.Unix(int64(defaultBatchSize), 0),
			end:       time.Unix(int64(3*defaultBatchSize), 0),
			expected:  selectEntries(flattenEntries(chunkData), defaultBatchSize, 3*defaultBatchSize),
		},
		{
			name:      "backward direction, partial range",
			direction: logproto.BACKWARD,
			start:     time.Unix(int64(defaultBatchSize), 0),
			end:       time.Unix(int64(3*defaultBatchSize), 0),
			expected:  reverseEntries(selectEntries(flattenEntries(chunkData), defaultBatchSize, 3*defaultBatchSize)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := syntax.ParseLogSelector(`{app=~".+"}`, false)
			require.NoError(t, err)

			pipeline, err := expr.Pipeline()
			require.NoError(t, err)

			iterator := NewChunksEntryIterator(ctx, storage, chks, pipeline, tt.direction, tt.start.UnixNano(), tt.end.UnixNano())

			result := iterateEntries(iterator)
			require.NoError(t, iterator.Close())
			require.NoError(t, iterator.Err())

			assertEqualEntries(t, tt.expected, result)
		})
	}
}

func TestChunksSampleIterator(t *testing.T) {
	ctx := context.Background()
	storage := &mockBlockStorage{data: make(map[string][]byte)}

	// Generate test data with multiple batches
	chunkData := generateTestChunkData(5 * defaultBatchSize)
	chks := writeChunksToStorage(t, storage, chunkData)

	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
		expected []logproto.Sample
	}{
		{
			name:     "all samples",
			start:    time.Unix(0, 0),
			end:      time.Unix(int64(5*defaultBatchSize+1), 0),
			expected: entriesToSamples(flattenEntries(chunkData)),
		},
		{
			name:     "partial range",
			start:    time.Unix(int64(defaultBatchSize), 0),
			end:      time.Unix(int64(3*defaultBatchSize), 0),
			expected: entriesToSamples(selectEntries(flattenEntries(chunkData), defaultBatchSize, 3*defaultBatchSize)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := syntax.ParseSampleExpr(`count_over_time({app=~".+"} [1m])`)
			require.NoError(t, err)

			extractor, err := expr.Extractor()
			require.NoError(t, err)
			iterator := NewChunksSampleIterator(ctx, storage, chks, extractor, tt.start.UnixNano(), tt.end.UnixNano())

			result := iterateSamples(iterator)
			require.NoError(t, iterator.Close())
			require.NoError(t, iterator.Err())

			assertEqualSamples(t, tt.expected, result)
		})
	}
}

func TestSortChunks(t *testing.T) {
	chks := []ChunkData{
		{
			meta:   &walchunks.Meta{MinTime: 2, MaxTime: 4},
			labels: labels.FromStrings("app", "test1"),
		},
		{
			meta:   &walchunks.Meta{MinTime: 1, MaxTime: 3},
			labels: labels.FromStrings("app", "test2"),
		},
		{
			meta:   &walchunks.Meta{MinTime: 1, MaxTime: 3},
			labels: labels.FromStrings("app", "test1"),
		},
	}

	t.Run("forward direction", func(t *testing.T) {
		sortChunks(chks, logproto.FORWARD)
		require.Equal(t, int64(1), chks[0].meta.MinTime)
		require.Equal(t, "test1", chks[0].labels.Get("app"))
		require.Equal(t, int64(1), chks[1].meta.MinTime)
		require.Equal(t, "test2", chks[1].labels.Get("app"))
		require.Equal(t, int64(2), chks[2].meta.MinTime)
	})

	t.Run("backward direction", func(t *testing.T) {
		sortChunks(chks, logproto.BACKWARD)
		require.Equal(t, int64(4), chks[0].meta.MaxTime)
		require.Equal(t, "test1", chks[0].labels.Get("app"))
		require.Equal(t, int64(3), chks[1].meta.MaxTime)
		require.Equal(t, "test1", chks[1].labels.Get("app"))
		require.Equal(t, int64(3), chks[2].meta.MaxTime)
		require.Equal(t, "test2", chks[2].labels.Get("app"))
	})
}

func TestIsOverlapping(t *testing.T) {
	tests := []struct {
		name      string
		first     ChunkData
		second    ChunkData
		direction logproto.Direction
		expected  bool
	}{
		{
			name:      "overlapping forward",
			first:     ChunkData{meta: &walchunks.Meta{MinTime: 1, MaxTime: 3}},
			second:    ChunkData{meta: &walchunks.Meta{MinTime: 2, MaxTime: 4}},
			direction: logproto.FORWARD,
			expected:  true,
		},
		{
			name:      "non-overlapping forward",
			first:     ChunkData{meta: &walchunks.Meta{MinTime: 1, MaxTime: 2}},
			second:    ChunkData{meta: &walchunks.Meta{MinTime: 3, MaxTime: 4}},
			direction: logproto.FORWARD,
			expected:  false,
		},
		{
			name:      "overlapping backward",
			first:     ChunkData{meta: &walchunks.Meta{MinTime: 2, MaxTime: 4}},
			second:    ChunkData{meta: &walchunks.Meta{MinTime: 1, MaxTime: 3}},
			direction: logproto.BACKWARD,
			expected:  true,
		},
		{
			name:      "non-overlapping backward",
			first:     ChunkData{meta: &walchunks.Meta{MinTime: 3, MaxTime: 4}},
			second:    ChunkData{meta: &walchunks.Meta{MinTime: 1, MaxTime: 2}},
			direction: logproto.BACKWARD,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isOverlapping(tt.first, tt.second, tt.direction)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestBaseChunkIterator(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name      string
		chunks    []ChunkData
		direction logproto.Direction
		expected  [][]ChunkData
	}{
		{
			name: "Forward, non-overlapping",
			chunks: []ChunkData{
				newTestChunkData("1", 100, 200),
				newTestChunkData("2", 300, 400),
				newTestChunkData("3", 500, 600),
				newTestChunkData("4", 700, 800),
			},
			direction: logproto.FORWARD,
			expected: [][]ChunkData{
				{newTestChunkData("1", 100, 200), newTestChunkData("2", 300, 400)},
				{newTestChunkData("3", 500, 600), newTestChunkData("4", 700, 800)},
			},
		},
		{
			name: "Backward, non-overlapping",
			chunks: []ChunkData{
				newTestChunkData("4", 700, 800),
				newTestChunkData("3", 500, 600),
				newTestChunkData("2", 300, 400),
				newTestChunkData("1", 100, 200),
			},
			direction: logproto.BACKWARD,
			expected: [][]ChunkData{
				{newTestChunkData("4", 700, 800), newTestChunkData("3", 500, 600)},
				{newTestChunkData("2", 300, 400), newTestChunkData("1", 100, 200)},
			},
		},
		{
			name: "Forward, overlapping",
			chunks: []ChunkData{
				newTestChunkData("1", 100, 300),
				newTestChunkData("2", 200, 400),
				newTestChunkData("3", 350, 550),
				newTestChunkData("4", 600, 800),
			},
			direction: logproto.FORWARD,
			expected: [][]ChunkData{
				{newTestChunkData("1", 100, 300), newTestChunkData("2", 200, 400), newTestChunkData("3", 350, 550)},
				{newTestChunkData("4", 600, 800)},
			},
		},
		{
			name: "Backward, overlapping",
			chunks: []ChunkData{
				newTestChunkData("4", 600, 800),
				newTestChunkData("3", 350, 550),
				newTestChunkData("2", 200, 400),
				newTestChunkData("1", 100, 300),
				newTestChunkData("0", 10, 20),
			},
			direction: logproto.BACKWARD,
			expected: [][]ChunkData{
				{newTestChunkData("4", 600, 800), newTestChunkData("3", 350, 550), newTestChunkData("2", 200, 400), newTestChunkData("1", 100, 300)},
				{newTestChunkData("0", 10, 20)},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			iter := &testBaseChunkIterator{
				baseChunksIterator: baseChunksIterator[*testIterator]{
					ctx:       ctx,
					chunks:    tc.chunks,
					direction: tc.direction,
					bachSize:  2,
					batch:     make([]ChunkData, 0, 2),
					iteratorFactory: func(chunks []ChunkData) (*testIterator, error) {
						return &testIterator{chunks: chunks}, nil
					},
					isNil: func(it *testIterator) bool { return it == nil },
				},
			}
			var batches [][]ChunkData
			for len(iter.chunks) > 0 {
				err := iter.nextBatch()
				require.NoError(t, err)

				batch := make([]ChunkData, len(iter.batch))
				copy(batch, iter.batch)
				batches = append(batches, batch)
			}

			require.Equal(t, tc.expected, batches)
		})
	}
}

// Helper functions and types

type testBaseChunkIterator struct {
	baseChunksIterator[*testIterator]
}

type testIterator struct {
	chunks []ChunkData
	index  int
}

func (t *testIterator) Next() bool {
	t.index++
	return t.index < len(t.chunks)
}

func (t *testIterator) Close() error       { return nil }
func (t *testIterator) Err() error         { return nil }
func (t *testIterator) StreamHash() uint64 { return 0 }
func (t *testIterator) Labels() string     { return "" }
func (t *testIterator) At() logproto.Entry { return logproto.Entry{} }

func newTestChunkData(id string, minTime, maxTime int64) ChunkData {
	return ChunkData{
		id: id,
		meta: &walchunks.Meta{
			MinTime: minTime,
			MaxTime: maxTime,
		},
		labels: labels.Labels{},
	}
}

func createChunk(minTime, maxTime int64, labelName, labelValue string) ChunkData {
	return ChunkData{
		meta: &walchunks.Meta{
			MinTime: minTime,
			MaxTime: maxTime,
		},
		labels: labels.FromStrings(labelName, labelValue),
	}
}

func assertEqualChunks(t *testing.T, expected, actual ChunkData) {
	require.Equal(t, expected.meta.MinTime, actual.meta.MinTime, "MinTime mismatch")
	require.Equal(t, expected.meta.MaxTime, actual.meta.MaxTime, "MaxTime mismatch")
	require.Equal(t, expected.labels, actual.labels, "Labels mismatch")
}

func generateTestChunkData(totalEntries int) []struct {
	labels  labels.Labels
	entries []*logproto.Entry
} {
	var chunkData []struct {
		labels  labels.Labels
		entries []*logproto.Entry
	}

	entriesPerChunk := defaultBatchSize * 2 // Each chunk will contain 2 batches worth of entries
	numChunks := (totalEntries + entriesPerChunk - 1) / entriesPerChunk

	for i := 0; i < numChunks; i++ {
		startIndex := i * entriesPerChunk
		endIndex := (i + 1) * entriesPerChunk
		if endIndex > totalEntries {
			endIndex = totalEntries
		}

		chunkData = append(chunkData, struct {
			labels  labels.Labels
			entries []*logproto.Entry
		}{
			labels:  labels.FromStrings("app", fmt.Sprintf("test%d", i)),
			entries: generateEntries(startIndex, endIndex-1),
		})
	}

	return chunkData
}

func writeChunksToStorage(t *testing.T, storage *mockBlockStorage, chunkData []struct {
	labels  labels.Labels
	entries []*logproto.Entry
},
) []ChunkData {
	chks := make([]ChunkData, 0, len(chunkData))
	for i, cd := range chunkData {
		var buf bytes.Buffer
		chunkID := fmt.Sprintf("chunk%d", i)
		_, err := walchunks.WriteChunk(&buf, cd.entries, walchunks.EncodingSnappy)
		require.NoError(t, err)

		storage.data[wal.Dir+chunkID] = buf.Bytes()
		chks = append(chks, newChunkData(chunkID, labelsToScratchBuilder(cd.labels), &walchunks.Meta{
			Ref:     walchunks.NewChunkRef(0, uint64(buf.Len())),
			MinTime: cd.entries[0].Timestamp.UnixNano(),
			MaxTime: cd.entries[len(cd.entries)-1].Timestamp.UnixNano(),
		}))
	}
	return chks
}

func generateEntries(start, end int) []*logproto.Entry {
	var entries []*logproto.Entry
	for i := start; i <= end; i++ {
		entries = append(entries, &logproto.Entry{
			Timestamp: time.Unix(int64(i), 0),
			Line:      fmt.Sprintf("line%d", i),
		})
	}
	return entries
}

func flattenEntries(chunkData []struct {
	labels  labels.Labels
	entries []*logproto.Entry
},
) []logproto.Entry {
	var result []logproto.Entry
	for _, cd := range chunkData {
		for _, e := range cd.entries {
			result = append(result, logproto.Entry{Timestamp: e.Timestamp, Line: e.Line})
		}
	}
	return result
}

func reverseEntries(entries []logproto.Entry) []logproto.Entry {
	for i := 0; i < len(entries)/2; i++ {
		j := len(entries) - 1 - i
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries
}

func selectEntries(entries []logproto.Entry, start, end int) []logproto.Entry {
	var result []logproto.Entry
	for _, e := range entries {
		if e.Timestamp.Unix() >= int64(start) && e.Timestamp.Unix() < int64(end) {
			result = append(result, e)
		}
	}
	return result
}

func entriesToSamples(entries []logproto.Entry) []logproto.Sample {
	var samples []logproto.Sample
	for _, e := range entries {
		samples = append(samples, logproto.Sample{
			Timestamp: e.Timestamp.UnixNano(),
			Value:     float64(1), // Use timestamp as value for simplicity
		})
	}
	return samples
}

func iterateEntries(iterator *ChunksEntryIterator[iter.EntryIterator]) []logproto.Entry {
	var result []logproto.Entry
	for iterator.Next() {
		entry := iterator.At()
		result = append(result, logproto.Entry{Timestamp: entry.Timestamp, Line: entry.Line})
	}
	return result
}

func iterateSamples(iterator *ChunksSampleIterator[iter.SampleIterator]) []logproto.Sample {
	var result []logproto.Sample
	for iterator.Next() {
		result = append(result, iterator.At())
	}
	return result
}

func assertEqualEntries(t *testing.T, expected, actual []logproto.Entry) {
	require.Equal(t, len(expected), len(actual), "Number of entries mismatch")
	for i := range expected {
		require.Equal(t, expected[i].Timestamp, actual[i].Timestamp, "Timestamp mismatch at index %d", i)
		require.Equal(t, expected[i].Line, actual[i].Line, "Line mismatch at index %d", i)
	}
}

func assertEqualSamples(t *testing.T, expected, actual []logproto.Sample) {
	require.Equal(t, len(expected), len(actual), "Number of samples mismatch")
	for i := range expected {
		require.Equal(t, expected[i].Timestamp, actual[i].Timestamp, "Timestamp mismatch at index %d", i)
		require.Equal(t, expected[i].Value, actual[i].Value, "Value mismatch at index %d", i)
	}
}

func labelsToScratchBuilder(lbs labels.Labels) *labels.ScratchBuilder {
	sb := labels.NewScratchBuilder(len(lbs))
	sb.Reset()
	for i := 0; i < len(lbs); i++ {
		sb.Add(lbs[i].Name, lbs[i].Value)
	}
	return &sb
}
