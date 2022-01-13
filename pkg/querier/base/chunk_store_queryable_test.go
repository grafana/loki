package base

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	promchunk "github.com/grafana/loki/pkg/storage/chunk/encoding"
)

// Make sure that chunkSeries implements SeriesWithChunks
var _ SeriesWithChunks = &chunkSeries{}

func TestChunkQueryable(t *testing.T) {
	for _, testcase := range testcases {
		for _, encoding := range encodings {
			for _, query := range queries {
				t.Run(fmt.Sprintf("%s/%s/%s", testcase.name, encoding.name, query.query), func(t *testing.T) {
					store, from := makeMockChunkStore(t, 24, encoding.e)
					queryable := newChunkStoreQueryable(store, testcase.f)
					testRangeQuery(t, queryable, from, query)
				})
			}
		}
	}
}

type mockChunkStore struct {
	chunks []chunk.Chunk
}

func (m mockChunkStore) Get(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]chunk.Chunk, error) {
	return m.chunks, nil
}

func makeMockChunkStore(t require.TestingT, numChunks int, encoding promchunk.Encoding) (mockChunkStore, model.Time) {
	var (
		chunks = make([]chunk.Chunk, 0, numChunks)
		from   = model.Time(0)
	)
	for i := 0; i < numChunks; i++ {
		c := mkChunk(t, from, from.Add(samplesPerChunk*sampleRate), sampleRate, encoding)
		chunks = append(chunks, c)
		from = from.Add(chunkOffset)
	}
	return mockChunkStore{chunks}, from
}

func mkChunk(t require.TestingT, mint, maxt model.Time, step time.Duration, encoding promchunk.Encoding) chunk.Chunk {
	metric := labels.Labels{
		{Name: model.MetricNameLabel, Value: "foo"},
	}
	pc, err := promchunk.NewForEncoding(encoding)
	require.NoError(t, err)
	for i := mint; i.Before(maxt); i = i.Add(step) {
		nc, err := pc.Add(model.SamplePair{
			Timestamp: i,
			Value:     model.SampleValue(float64(i)),
		})
		require.NoError(t, err)
		require.Nil(t, nc)
	}
	return chunk.NewChunk(userID, fp, metric, pc, mint, maxt)
}

func TestPartitionChunksOutputIsSortedByLabels(t *testing.T) {
	var allChunks []chunk.Chunk

	const count = 10
	// go down, to add series in reversed order
	for i := count; i > 0; i-- {
		ch := mkChunk(t, model.Time(0), model.Time(1000), time.Millisecond, promchunk.Bigchunk)
		// mkChunk uses `foo` as metric name, so we rename metric to be unique
		ch.Metric[0].Value = fmt.Sprintf("%02d", i)

		allChunks = append(allChunks, ch)
	}

	res := partitionChunks(allChunks, 0, 1000, mergeChunks)

	// collect labels from each series
	var seriesLabels []labels.Labels
	for res.Next() {
		seriesLabels = append(seriesLabels, res.At().Labels())
	}

	require.Len(t, seriesLabels, count)
	require.True(t, sort.IsSorted(sortedByLabels(seriesLabels)))
}

type sortedByLabels []labels.Labels

func (b sortedByLabels) Len() int           { return len(b) }
func (b sortedByLabels) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b sortedByLabels) Less(i, j int) bool { return labels.Compare(b[i], b[j]) < 0 }
