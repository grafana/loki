package chunkenc

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
)

// TestMergeDedupsEntriesAcrossStreamShards reproduces a line that automatic
// stream sharding split across two shards: the same line lands in two streams
// that differ only in __stream_shard__. Merging the shard iterators must return
// the line once for log queries and its sample once for metric queries.
func TestMergeDedupsEntriesAcrossStreamShards(t *testing.T) {
	shard0 := labels.FromStrings("__stream_shard__", "0", "app", "foo")
	shard1 := labels.FromStrings("__stream_shard__", "1", "app", "foo")

	newChunk := func(t *testing.T) *MemChunk {
		c := NewMemChunk(ChunkFormatV4, compression.GZIP, DefaultTestHeadBlockFmt, testBlockSize, testTargetSize)
		dup, err := c.Append(logprotoEntry(1e9, "duplicate line"))
		require.NoError(t, err)
		require.False(t, dup)
		return c
	}
	c0, c1 := newChunk(t), newChunk(t)
	from, through := time.Unix(0, 0), time.Unix(0, 2e9)

	t.Run("logs", func(t *testing.T) {
		pipeline := log.NewNoopPipeline()
		it0, err := c0.Iterator(context.Background(), from, through, logproto.FORWARD, pipeline.ForStream(shard0))
		require.NoError(t, err)
		it1, err := c1.Iterator(context.Background(), from, through, logproto.FORWARD, pipeline.ForStream(shard1))
		require.NoError(t, err)

		merged := iter.NewMergeEntryIterator(context.Background(), []iter.EntryIterator{it0, it1}, logproto.FORWARD)
		var lines []string
		for merged.Next() {
			lines = append(lines, merged.At().Line)
		}
		require.NoError(t, merged.Err())
		require.Equal(t, []string{"duplicate line"}, lines)
	})

	t.Run("samples", func(t *testing.T) {
		extractor, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
		require.NoError(t, err)

		merged := iter.NewMergeSampleIterator(context.Background(), []iter.SampleIterator{
			c0.SampleIterator(context.Background(), from, through, extractor.ForStream(shard0)),
			c1.SampleIterator(context.Background(), from, through, extractor.ForStream(shard1)),
		})
		var samples int
		for merged.Next() {
			samples++
		}
		require.NoError(t, merged.Err())
		require.Equal(t, 1, samples, "the duplicate sample must be dropped across shards")
	})

	// When only the store (or only the ingesters) is queried, the shards arrive
	// sort-merged within a single iterator, and the merge iterator has to
	// deduplicate them there.
	t.Run("logs from a single sorted iterator", func(t *testing.T) {
		pipeline := log.NewNoopPipeline()
		it0, err := c0.Iterator(context.Background(), from, through, logproto.FORWARD, pipeline.ForStream(shard0))
		require.NoError(t, err)
		it1, err := c1.Iterator(context.Background(), from, through, logproto.FORWARD, pipeline.ForStream(shard1))
		require.NoError(t, err)
		sorted := iter.NewSortEntryIterator([]iter.EntryIterator{it0, it1}, logproto.FORWARD)

		merged := iter.NewMergeEntryIterator(context.Background(), []iter.EntryIterator{sorted}, logproto.FORWARD)
		var entries int
		for merged.Next() {
			entries++
		}
		require.NoError(t, merged.Err())
		require.Equal(t, 1, entries)
	})

	t.Run("samples from a single sorted iterator", func(t *testing.T) {
		extractor, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
		require.NoError(t, err)
		sorted := iter.NewSortSampleIterator([]iter.SampleIterator{
			c0.SampleIterator(context.Background(), from, through, extractor.ForStream(shard0)),
			c1.SampleIterator(context.Background(), from, through, extractor.ForStream(shard1)),
		})

		merged := iter.NewMergeSampleIterator(context.Background(), []iter.SampleIterator{sorted})
		var samples int
		for merged.Next() {
			samples++
		}
		require.NoError(t, merged.Err())
		require.Equal(t, 1, samples)
	})
}
