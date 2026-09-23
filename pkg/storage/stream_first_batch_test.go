package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

// TestLokiStore_SelectSamples_StreamFirstMatchesTimestampFirst verifies stream-first and
// timestamp-first return the same samples through the full SelectSamples path.
func TestLokiStore_SelectSamples_StreamFirstMatchesTimestampFirst(t *testing.T) {
	periodConfig := config.PeriodConfig{From: config.DayTime{Time: 0}, Schema: "v11"}
	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	const (
		streamCount     = 150
		chunksPerStream = 3
		logsPerChunk    = 5
	)

	var (
		streams      = manyStreamsFixture(streamCount, chunksPerStream, logsPerChunk)
		totalSamples = streamCount * chunksPerStream * logsPerChunk
		query        = `count_over_time({foo=~".+"}[1m])`
		start, end   = time.Unix(0, 0), time.Unix(0, int64(chunksPerStream*logsPerChunk+1))
	)

	type samplePoint struct {
		labels string
		ts     int64
		value  float64
	}

	// selectSamples collects (labels, ts, value) per sample, so a misattributed sample shows up
	// as a content mismatch, not just a matching count. MaxParallelGetChunk is set high enough
	// to exercise concurrent prefetch workers.
	selectSamples := func(t *testing.T, order logproto.SampleOrder, shards []astmapper.ShardAnnotation) []samplePoint {
		st := &LokiStore{
			chunkMetrics: NilMetrics,
			cfg:          Config{MaxChunkBatchSize: 50, MaxParallelGetChunk: 150},
			Store:        newMockChunkStore(chunkfmt, headfmt, streams),
		}
		_, ctx := stats.NewContext(user.InjectOrgID(context.Background(), "fake"))
		req := newSampleQuery(query, start, end, shards, nil)
		req.Order = order
		it, err := st.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: req})
		require.NoError(t, err)
		var got []samplePoint
		for it.Next() {
			sm := it.At()
			got = append(got, samplePoint{labels: it.Labels(), ts: sm.Timestamp, value: sm.Value})
		}
		require.NoError(t, it.Err())
		require.NoError(t, it.Close())
		return got
	}

	tsSamples := selectSamples(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, nil)
	sfSamples := selectSamples(t, logproto.SAMPLE_ORDER_BY_STREAM, nil)

	require.Len(t, tsSamples, totalSamples, "sanity: timestamp-first must read every sample")
	require.ElementsMatch(t, tsSamples, sfSamples, "stream-first returned different samples than timestamp-first")

	// A shard must not change the samples returned: the reader strips the injected shard matcher
	// before matching, without corrupting the matchers slice shared across streams.
	t.Run("with a shard annotation", func(t *testing.T) {
		shard := []astmapper.ShardAnnotation{{Shard: 0, Of: 1}} // 0_of_1 selects everything
		tsSharded := selectSamples(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, shard)
		sfSharded := selectSamples(t, logproto.SAMPLE_ORDER_BY_STREAM, shard)
		require.Len(t, tsSharded, totalSamples, "sanity: sharded timestamp-first must read every sample")
		require.ElementsMatch(t, tsSharded, sfSharded, "sharded stream-first returned different samples (shard matcher not stripped?)")
	})

	// Exercises the cross-source merge with two disjoint stores, not just the single-iterator
	// shortcut.
	t.Run("through cross-source stream-first merge", func(t *testing.T) {
		_, ctx := stats.NewContext(user.InjectOrgID(context.Background(), "fake"))
		half := len(streams) / 2
		selectSamples := func(t *testing.T, ss []*logproto.Stream) iter.SampleIterator {
			st := &LokiStore{chunkMetrics: NilMetrics, cfg: Config{MaxChunkBatchSize: 50}, Store: newMockChunkStore(chunkfmt, headfmt, ss)}
			req := newSampleQuery(query, start, end, nil, nil)
			req.Order = logproto.SAMPLE_ORDER_BY_STREAM
			it, err := st.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: req})
			require.NoError(t, err)
			return it
		}
		merged := iter.NewStreamFirstMergeSampleIterator(ctx, []iter.SampleIterator{
			selectSamples(t, streams[:half]), selectSamples(t, streams[half:]),
		})
		var n int
		for merged.Next() {
			n++
		}
		require.NoError(t, merged.Err())
		require.NoError(t, merged.Close())
		require.Equal(t, totalSamples, n, "cross-source stream-first merge dropped samples")
	})
}

// TestLokiStore_SelectSamples_StreamFirst_ReleaseDoesNotRaceAbandonedFetch verifies releaseStream
// never nils a chunk's Data while that chunk's own fetch is still in flight.
func TestLokiStore_SelectSamples_StreamFirst_ReleaseDoesNotRaceAbandonedFetch(t *testing.T) {
	periodConfig := config.PeriodConfig{From: config.DayTime{Time: 0}, Schema: "v11"}
	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	const chunkCount = 40
	streams := make([]*logproto.Stream, chunkCount)
	for i := 0; i < chunkCount; i++ {
		streams[i] = &logproto.Stream{
			Labels:  `{foo="bar"}`,
			Entries: []logproto.Entry{{Timestamp: time.Unix(0, int64(i)*int64(time.Millisecond)), Line: "a"}},
		}
	}

	st := &LokiStore{
		chunkMetrics: NilMetrics,
		cfg:          Config{MaxChunkBatchSize: 1, MaxParallelGetChunk: 1},
		Store:        newMockChunkStore(chunkfmt, headfmt, streams),
	}
	ctx, cancel := context.WithCancel(user.InjectOrgID(context.Background(), "fake"))
	defer cancel()

	req := newSampleQuery(`count_over_time({foo=~".+"}[1m])`, time.Unix(0, 0), time.Unix(0, int64(chunkCount)*int64(time.Millisecond)+1), nil, nil)
	req.Order = logproto.SAMPLE_ORDER_BY_STREAM
	rawIt, err := st.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: req})
	require.NoError(t, err)
	it, ok := rawIt.(*lazyStreamFirstSampleIterator)
	require.True(t, ok, "test relies on the concrete type to inspect release state")

	// All 40 streams share the same labels, so the store groups them into this one stream.
	require.Len(t, it.streamChunks, 1)
	chunks := it.streamChunks[0]

	require.True(t, it.Next(), "must decode at least one sample before canceling")
	cancel()
	for it.Next() { //nolint:revive // draining until cancellation is the point.
	}
	require.NoError(t, it.Close())

	for _, c := range chunks {
		require.Nil(t, c.Chunk.Data, "the abandoned stream's Data must still be released")
	}
}

func TestLokiStore_SelectSamples_StreamFirst_ReleaseFetchedData(t *testing.T) {
	periodConfig := config.PeriodConfig{From: config.DayTime{Time: 0}, Schema: "v11"}
	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	const (
		streamCount     = 5
		chunksPerStream = 2
		logsPerChunk    = 3
	)
	streams := manyStreamsFixture(streamCount, chunksPerStream, logsPerChunk)

	st := &LokiStore{
		chunkMetrics: NilMetrics,
		cfg:          Config{MaxChunkBatchSize: 2, MaxParallelGetChunk: 4},
		Store:        newMockChunkStore(chunkfmt, headfmt, streams),
	}
	ctx := user.InjectOrgID(context.Background(), "fake")
	req := newSampleQuery(`count_over_time({foo=~".+"}[1m])`, time.Unix(0, 0), time.Unix(0, int64(chunksPerStream*logsPerChunk+1)), nil, nil)
	req.Order = logproto.SAMPLE_ORDER_BY_STREAM

	rawIt, err := st.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: req})
	require.NoError(t, err)
	it, ok := rawIt.(*lazyStreamFirstSampleIterator)
	require.True(t, ok, "test relies on the concrete type to inspect per-stream release timing")

	// Snapshot each stream's chunks before draining: releaseStream nils it.streamChunks[idx] in
	// place.
	chunksBeforeRelease := make([][]*LazyChunk, len(it.streamChunks))
	copy(chunksBeforeRelease, it.streamChunks)

	var n, lastIdx int
	for it.Next() {
		_ = it.At()
		n++

		if it.idx > lastIdx {
			for i := lastIdx; i < it.idx; i++ {
				for _, c := range chunksBeforeRelease[i] {
					require.Nil(t, c.Chunk.Data, "stream %d's fetched Data must be released before stream %d starts", i, it.idx)
				}
			}
			lastIdx = it.idx
		}
	}
	require.NoError(t, it.Err())
	require.NoError(t, it.Close())
	require.Positive(t, n, "expected samples; got none")

	// The loop above never checks the last stream; verify it's released too.
	for i, chunks := range chunksBeforeRelease {
		for _, c := range chunks {
			require.Nil(t, c.Chunk.Data, "stream %d's fetched Data must be released after it is consumed", i)
		}
	}
}

// BenchmarkLokiStore_SelectSamples compares stream-first against timestamp-first sample reading,
// reporting bytes/allocations per call with -benchmem. Lower bytes/op for stream-first is the
// expected signature of not holding one decoded chunk per concurrently-read stream.
func BenchmarkLokiStore_SelectSamples(b *testing.B) {
	periodConfig := config.PeriodConfig{From: config.DayTime{Time: 0}, Schema: "v11"}
	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(b, err)

	const (
		streamCount     = 150
		chunksPerStream = 3
		logsPerChunk    = 5
	)
	streams := manyStreamsFixture(streamCount, chunksPerStream, logsPerChunk)
	query := `count_over_time({foo=~".+"}[1m])`
	start, end := time.Unix(0, 0), time.Unix(0, int64(chunksPerStream*logsPerChunk+1))

	run := func(b *testing.B, order logproto.SampleOrder) {
		req := newSampleQuery(query, start, end, nil, nil)
		req.Order = order
		ctx := user.InjectOrgID(context.Background(), "fake")

		b.ReportAllocs()
		for b.Loop() {
			st := &LokiStore{
				chunkMetrics: NilMetrics,
				cfg:          Config{MaxChunkBatchSize: 50, MaxParallelGetChunk: 150},
				Store:        newMockChunkStore(chunkfmt, headfmt, streams),
			}
			it, err := st.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: req})
			if err != nil {
				b.Fatal(err)
			}
			for it.Next() { //nolint:revive // draining the iterator is the point.
			}
			if err := it.Err(); err != nil {
				b.Fatal(err)
			}
			if err := it.Close(); err != nil {
				b.Fatal(err)
			}
		}
	}

	b.Run("timestamp-first", func(b *testing.B) { run(b, logproto.SAMPLE_ORDER_BY_TIMESTAMP) })
	b.Run("stream-first", func(b *testing.B) { run(b, logproto.SAMPLE_ORDER_BY_STREAM) })
}

func TestLazyStreamFirstSampleIterator(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	periodConfig := config.PeriodConfig{From: config.DayTime{Time: 0}, Schema: "v11"}
	schemaConfig := config.SchemaConfig{Configs: []config.PeriodConfig{periodConfig}}
	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	newEx := func() log.SampleExtractor {
		ex, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
		require.NoError(t, err)
		return ex
	}
	matchers := newMatchers(`{foo=~".+"}`)
	start, end := time.Unix(0, 0), time.Unix(0, 100*int64(time.Millisecond))

	// streamFirst builds a stream-first iterator with the given batching and fetch.
	streamFirst := func(ctx context.Context, chunks []*LazyChunk, batchSize, maxConcurrent int, fetch chunkFetchFunc) (iter.SampleIterator, error) {
		return newStreamFirstSampleBatchIterator(ctx, schemaConfig, NilMetrics, chunks, batchSize, matchers, start, end, nil, maxConcurrent, fetch, newEx(), iter.HintTimeRanges{})
	}

	// drainTimestamps drains it and returns each sample's timestamp, asserting a clean close.
	drainTimestamps := func(t *testing.T, it iter.SampleIterator) []int64 {
		var got []int64
		for it.Next() {
			got = append(got, it.At().Timestamp)
		}
		require.NoError(t, it.Err())
		require.NoError(t, it.Close())
		return got
	}

	// millisToNanos turns millisecond values into the nanosecond timestamps the iterator returns.
	millisToNanos := func(vals ...int64) []int64 {
		out := make([]int64, len(vals))
		for i, v := range vals {
			out[i] = v * time.Millisecond.Nanoseconds()
		}
		return out
	}

	t.Run("matches the timestamp-first iterator's deduplicated result, in stream-first order", func(t *testing.T) {
		// Three streams interleaved, plus a duplicate chunk for one stream to exercise dedup.
		buildChunks := func() []*LazyChunk {
			return []*LazyChunk{
				newLazyChunk(chunkfmt, headfmt, mkStream("b", 1, 2, 3)),
				newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3)),
				newLazyChunk(chunkfmt, headfmt, mkStream("c", 1, 2, 3)),
				newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3)), // duplicate of stream "a"
			}
		}
		type entry struct {
			hash   uint64
			labels string
			ts     int64
			value  float64
		}
		drain := func(it iter.SampleIterator) []entry {
			var out []entry
			for it.Next() {
				sm := it.At()
				out = append(out, entry{it.StreamHash(), it.Labels(), sm.Timestamp, sm.Value})
			}
			require.NoError(t, it.Err())
			require.NoError(t, it.Close())
			return out
		}

		timestampFirstIterator, err := newTimestampFirstSampleBatchIterator(context.Background(), schemaConfig, NilMetrics, buildChunks(), 10, matchers, start, end, nil, newEx(), iter.HintTimeRanges{})
		require.NoError(t, err)
		streamFirstIterator, err := streamFirst(context.Background(), buildChunks(), 10, 0, fetchLazyChunks)
		require.NoError(t, err)

		timestampFirstEntries := drain(timestampFirstIterator)
		streamFirstEntries := drain(streamFirstIterator)

		// Same data regardless of order; streamHash differs by design (extractor hash vs. raw
		// fingerprint), so compare only (labels, ts, value).
		type point struct {
			labels string
			ts     int64
			value  float64
		}
		points := func(es []entry) []point {
			out := make([]point, len(es))
			for i, e := range es {
				out[i] = point{e.labels, e.ts, e.value}
			}
			return out
		}
		require.ElementsMatch(t, points(timestampFirstEntries), points(streamFirstEntries))
		require.NotEmpty(t, streamFirstEntries)

		// Stream-first ordering: streamHash is non-decreasing; within one streamHash, ts ascending.
		for i := 1; i < len(streamFirstEntries); i++ {
			if streamFirstEntries[i].hash == streamFirstEntries[i-1].hash {
				require.LessOrEqualf(t, streamFirstEntries[i-1].ts, streamFirstEntries[i].ts, "ts not ascending within stream at %d", i)
			} else {
				require.Lessf(t, streamFirstEntries[i-1].hash, streamFirstEntries[i].hash, "streamHash not ascending at %d", i)
			}
		}
	})

	t.Run("applies hint ranges to every stream", func(t *testing.T) {
		chunks := []*LazyChunk{
			newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3, 4, 5)),
			newLazyChunk(chunkfmt, headfmt, mkStream("b", 1, 2, 3, 4, 5)),
		}
		hintRanges := iter.NewHintTimeRanges(
			[]logproto.HintTimeRange{{
				Start: start.Add(2 * time.Millisecond),
				End:   start.Add(4 * time.Millisecond),
			}},
			start,
			end,
		)

		it, err := newStreamFirstSampleBatchIterator(
			context.Background(), schemaConfig, NilMetrics, chunks, 2, matchers,
			start, end, nil, 2, fetchLazyChunks, newEx(), hintRanges,
		)
		require.NoError(t, err)
		require.ElementsMatch(t, millisToNanos(2, 3, 2, 3), drainTimestamps(t, it))
	})

	t.Run("tracks decompressed bytes and lines like the timestamp-first iterator", func(t *testing.T) {
		// Both paths must decompress the same, non-zero bytes and lines, since stream-first
		// delegates per-stream decoding to the timestamp-first iterator. Head-chunk bytes stay
		// zero: the store reads only flushed chunks.
		buildChunks := func() []*LazyChunk {
			return []*LazyChunk{
				newLazyChunk(chunkfmt, headfmt, mkStream("b", 1, 2, 3)),
				newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3)),
				newLazyChunk(chunkfmt, headfmt, mkStream("c", 1, 2, 3)),
			}
		}
		drainStoreStats := func(t *testing.T, build func(ctx context.Context) (iter.SampleIterator, error)) stats.Result {
			statsCtx, ctx := stats.NewContext(context.Background())
			it, err := build(ctx)
			require.NoError(t, err)
			for it.Next() { //nolint:revive // draining the iterator is the point.
			}
			require.NoError(t, it.Err())
			require.NoError(t, it.Close())
			return statsCtx.Result(0, 0, 0)
		}

		timestampFirstStats := drainStoreStats(t, func(ctx context.Context) (iter.SampleIterator, error) {
			return newTimestampFirstSampleBatchIterator(ctx, schemaConfig, NilMetrics, buildChunks(), 10, matchers, start, end, nil, newEx(), iter.HintTimeRanges{})
		}).Querier.Store.Chunk
		streamFirstStats := drainStoreStats(t, func(ctx context.Context) (iter.SampleIterator, error) {
			return streamFirst(ctx, buildChunks(), 10, 0, fetchLazyChunks)
		}).Querier.Store.Chunk

		require.Positive(t, timestampFirstStats.DecompressedBytes, "sanity: the timestamp-first path must decompress something")
		require.Positive(t, timestampFirstStats.DecompressedLines)
		require.Equal(t, timestampFirstStats.DecompressedBytes, streamFirstStats.DecompressedBytes, "stream-first must decompress the same bytes")
		require.Equal(t, timestampFirstStats.DecompressedLines, streamFirstStats.DecompressedLines, "stream-first must decompress the same lines")
		require.Equal(t, timestampFirstStats.HeadChunkBytes, streamFirstStats.HeadChunkBytes)
		require.Zero(t, timestampFirstStats.HeadChunkBytes, "store path reads flushed chunks, so it records no head-chunk bytes")
		require.Zero(t, streamFirstStats.HeadChunkBytes, "store path reads flushed chunks, so it records no head-chunk bytes")
	})

	t.Run("reads non-overlapping chunks across multiple batches in order", func(t *testing.T) {
		chunks := []*LazyChunk{
			newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3)),
			newLazyChunk(chunkfmt, headfmt, mkStream("a", 4, 5, 6)),
			newLazyChunk(chunkfmt, headfmt, mkStream("a", 7, 8, 9)),
		}
		// batchSize 2 splits the 3 chunks across multiple prefetch batches.
		it, err := streamFirst(context.Background(), chunks, 2, 2, fetchLazyChunks)
		require.NoError(t, err)
		require.Equal(t, millisToNanos(1, 2, 3, 4, 5, 6, 7, 8, 9), drainTimestamps(t, it))
	})

	t.Run("merges and deduplicates time-overlapping chunks across multiple batches", func(t *testing.T) {
		// [1,2,3], [3,4,5] (overlap at ts 3) and a duplicate of [1,2,3] dedup to ts 1..5, across
		// a batchSize-2 boundary.
		chunks := []*LazyChunk{
			newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3)),
			newLazyChunk(chunkfmt, headfmt, mkStream("a", 3, 4, 5)),
			newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3)),
		}
		it, err := streamFirst(context.Background(), chunks, 2, 2, fetchLazyChunks)
		require.NoError(t, err)
		require.Equal(t, millisToNanos(1, 2, 3, 4, 5), drainTimestamps(t, it))
	})

	t.Run("reads all chunks in a single batch", func(t *testing.T) {
		chunks := []*LazyChunk{
			newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3)),
			newLazyChunk(chunkfmt, headfmt, mkStream("a", 4, 5, 6)),
		}

		var batches int
		fetch := func(ctx context.Context, s config.SchemaConfig, cs []*LazyChunk) error {
			batches++
			return fetchLazyChunks(ctx, s, cs)
		}

		// batchSize exceeds the chunk count, so it all fits in one prefetch batch.
		it, err := streamFirst(context.Background(), chunks, 10, 1, fetch)
		require.NoError(t, err)
		require.Equal(t, millisToNanos(1, 2, 3, 4, 5, 6), drainTimestamps(t, it))
		require.Equal(t, 1, batches, "batchSize exceeds the chunk count, so it all must fit in one prefetch batch")
	})

	t.Run("returns the context error when canceled while waiting for the preloader", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)

		fetching := make(chan struct{})
		fetch := func(ctx context.Context, _ config.SchemaConfig, _ []*LazyChunk) error {
			close(fetching) // a single chunk means fetch runs exactly once
			<-ctx.Done()    // block until the query is canceled
			return ctx.Err()
		}

		chunks := []*LazyChunk{newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3))}
		it, err := streamFirst(ctx, chunks, 2, 1, fetch)
		require.NoError(t, err)

		go func() {
			<-fetching
			cancel()
		}()

		require.False(t, it.Next())
		require.ErrorIs(t, it.Err(), context.Canceled)
		require.NoError(t, it.Close())
	})

	// Unlike the subtest above, this cancels after every chunk is prefetched and mid-decode, so
	// the per-stream sub-iterator's own ctx check must catch it, not waitUntilFetched.
	t.Run("returns the context error when canceled mid-decode of an already-fetched stream", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

		chunks := []*LazyChunk{
			newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3)),
			newLazyChunk(chunkfmt, headfmt, mkStream("b", 1, 2, 3)),
		}
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)

		it, err := streamFirst(ctx, chunks, 10, 1, fetchLazyChunks)
		require.NoError(t, err)

		require.True(t, it.Next(), "must decode stream 0's first sample before any cancellation")
		cancel()

		for it.Next() { //nolint:revive // draining until cancellation is the point.
		}
		require.ErrorIs(t, it.Err(), context.Canceled, "canceling mid-decode must not be reported as a clean end of input")
		require.NoError(t, it.Close())
	})

	t.Run("surfaces a preloader fetch error and stops iteration", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

		mockErr := errors.New("fetch failed")
		fetch := func(context.Context, config.SchemaConfig, []*LazyChunk) error { return mockErr }

		chunks := []*LazyChunk{newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3))}
		it, err := streamFirst(context.Background(), chunks, 2, 1, fetch)
		require.NoError(t, err)

		require.False(t, it.Next())
		require.ErrorIs(t, it.Err(), mockErr)
		require.NoError(t, it.Close())
	})

	// Proves a later stream's fetch starts while the consumer still decodes an earlier one, not
	// only once that stream finishes.
	t.Run("prefetches a later stream's batch while the consumer is still decoding an earlier one", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

		chunks := []*LazyChunk{
			newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3)),
			newLazyChunk(chunkfmt, headfmt, mkStream("b", 1, 2, 3)),
		}
		// Guard the assumption that chunks[1]'s stream sorts after chunks[0]'s: if that ever
		// flips, blocking chunks[1] below would block stream 0 and hang instead of fail.
		require.Less(t, chunks[0].Chunk.FingerprintModel(), chunks[1].Chunk.FingerprintModel())

		// Identify the second batch by chunk identity, not call order: dispatch order to workers
		// isn't guaranteed.
		secondBatchStarted := make(chan struct{})
		fetch := func(ctx context.Context, s config.SchemaConfig, cs []*LazyChunk) error {
			if len(cs) > 0 && cs[0] == chunks[1] {
				close(secondBatchStarted)
				<-ctx.Done() // hold the second batch open until the assertion below observes it
				return ctx.Err()
			}
			return fetchLazyChunks(ctx, s, cs)
		}

		// batchSize 1 makes batch 0 stream 0 and batch 1 stream 1; maxConcurrentBatches 2
		// dispatches both without waiting on the consumer.
		it, err := streamFirst(context.Background(), chunks, 1, 2, fetch)
		require.NoError(t, err)

		require.True(t, it.Next(), "must decode stream 0's first sample without waiting on stream 1's fetch")

		requireReceive(t, secondBatchStarted, "stream 1's batch to start fetching while stream 0 was still being consumed")

		require.NoError(t, it.Close())
	})

	// Proves Close, not just a canceled context, unblocks a pending Next: Close cancels the
	// preloader, reaching this stream's fetch the same way external cancellation would.
	//
	// it.cur and it.err aren't synchronized between Close and Next, so this doesn't attempt a
	// Next that succeeds on the stream Close is closing.
	t.Run("Close unblocks a Next call waiting on the preloader and stops its goroutines", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

		fetching := make(chan struct{})
		release := make(chan struct{})
		fetch := func(ctx context.Context, _ config.SchemaConfig, _ []*LazyChunk) error {
			close(fetching)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return ctx.Err()
		}

		chunks := []*LazyChunk{newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3))}
		it, err := streamFirst(context.Background(), chunks, 2, 1, fetch)
		require.NoError(t, err)
		defer close(release) // let the blocked fetch return once the test is done either way

		// Close and Next race on purpose. Report each on its own channel and assert from this
		// goroutine, since require's FailNow must not run on another one.
		closeErr := make(chan error, 1)
		go func() {
			<-fetching
			closeErr <- it.Close()
		}()
		nextDone := make(chan bool, 1)
		go func() {
			nextDone <- it.Next()
		}()

		require.False(t, requireReceive(t, nextDone, "Next to unblock once Close cancels the preloader"))
		require.NoError(t, requireReceive(t, closeErr, "Close to return"))
	})
}

// TestLazyStreamFirstSampleIterator_ClosesPreloaderBeforeCur verifies Close cancels the
// preloader before calling cur.Close, so a blocked in-flight fetch stops instead of continuing
// wastefully while cur.Close runs.
func TestLazyStreamFirstSampleIterator_ClosesPreloaderBeforeCur(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	batcher := newStreamFirstChunkBatcher(nil, 1)
	loader := newStreamFirstBatchLoader(config.SchemaConfig{}, NilMetrics, fetchLazyChunks)
	preloader := newStreamFirstChunkPreloader(context.Background(), batcher, loader, 1)

	curCloseStarted := make(chan struct{})
	releaseCurClose := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseCurClose) })
	defer release() // let the blocked Close call return even if an assertion below fails first
	cur := fakeSampleCloser{
		SampleIterator: iter.NoopSampleIterator,
		closeFunc: func() error {
			close(curCloseStarted)
			<-releaseCurClose
			return nil
		},
	}
	it := &lazyStreamFirstSampleIterator{preloader: preloader, cur: cur}

	closeDone := make(chan error, 1)
	go func() { closeDone <- it.Close() }()

	requireReceive(t, curCloseStarted, "cur.Close to start")
	require.Error(t, preloader.ctx.Err(), "the preloader must already be canceled before cur.Close starts")

	release()
	require.NoError(t, requireReceive(t, closeDone, "Close to return"))
}

// TestLazyStreamFirstSampleIterator_NextKeepsCurCloseErrorSeparateFromErr verifies a close error
// hit while Next rotates past a cleanly exhausted stream surfaces only through Close, not Err,
// the same separation the standalone Close method keeps.
func TestLazyStreamFirstSampleIterator_NextKeepsCurCloseErrorSeparateFromErr(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	batcher := newStreamFirstChunkBatcher(nil, 1)
	loader := newStreamFirstBatchLoader(config.SchemaConfig{}, NilMetrics, fetchLazyChunks)
	preloader := newStreamFirstChunkPreloader(context.Background(), batcher, loader, 1)

	closeBoom := errors.New("cur close failed")
	it := &lazyStreamFirstSampleIterator{
		streamChunks: [][]*LazyChunk{{}},
		preloader:    preloader,
		idx:          0,
		cur:          fakeSampleCloser{SampleIterator: iter.NoopSampleIterator, closeErr: closeBoom},
	}

	require.False(t, it.Next())
	require.NoError(t, it.Err(), "a close-time error must not leak into Err")
	require.ErrorIs(t, it.Close(), closeBoom)
}

// TestLazyStreamFirstSampleIterator_NextStaysFalseAfterError verifies a repeat call after an
// error stays false, instead of resuming into a later, already-preloaded stream.
func TestLazyStreamFirstSampleIterator_NextStaysFalseAfterError(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	periodConfig := config.PeriodConfig{From: config.DayTime{Time: 0}, Schema: "v11"}
	schemaConfig := config.SchemaConfig{Configs: []config.PeriodConfig{periodConfig}}
	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)
	ex, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
	require.NoError(t, err)

	batcher := newStreamFirstChunkBatcher(nil, 1)
	loader := newStreamFirstBatchLoader(config.SchemaConfig{}, NilMetrics, fetchLazyChunks)
	preloader := newStreamFirstChunkPreloader(context.Background(), batcher, loader, 1)

	boom := errors.New("stream 0 already failed")
	// Stream 1 holds a real, already-fetched chunk. If Next resumed into it after the preset
	// error below, it would decode successfully and return true.
	stream1 := []*LazyChunk{newLazyChunk(chunkfmt, headfmt, mkStream("b", 1))}

	it := &lazyStreamFirstSampleIterator{
		ctx:              context.Background(),
		schemas:          schemaConfig,
		metrics:          NilMetrics,
		batchSize:        10,
		matchers:         newMatchers(`{foo=~".+"}`),
		start:            time.Unix(0, 0),
		end:              time.Unix(0, 100*int64(time.Millisecond)),
		extractor:        ex,
		streamChunks:     [][]*LazyChunk{{}, stream1},
		streamEndIndexes: []int{0, 1},
		streamHashes:     []uint64{0, 1},
		preloader:        preloader,
		fetchedUpTo:      1, // already past every stream's end offset: waitUntilFetched never blocks
		idx:              0,
		err:              boom, // stream 0 already failed on an earlier call
	}
	defer it.Close()

	require.False(t, it.Next(), "must stay false once it.err is already set")
	require.ErrorIs(t, it.Err(), boom)

	require.False(t, it.Next(), "a repeat call must not resume into stream 1")
	require.ErrorIs(t, it.Err(), boom, "a repeat call must not lose the original error")
}

// TestLazyStreamFirstSampleIterator_ConsumerWaitMetricExcludesCanceledWaits verifies
// streamFirstConsumerWait observes only a wait that delivered a batch, not one that ended in
// cancellation.
func TestLazyStreamFirstSampleIterator_ConsumerWaitMetricExcludesCanceledWaits(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	periodConfig := config.PeriodConfig{From: config.DayTime{Time: 0}, Schema: "v11"}
	schemaConfig := config.SchemaConfig{Configs: []config.PeriodConfig{periodConfig}}
	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)
	ex, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
	require.NoError(t, err)

	// A fresh instance, not the shared NilMetrics, so its histogram reflects only this test.
	metrics := NewChunkMetrics(nil, 0)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	fetching := make(chan struct{})
	fetch := func(ctx context.Context, _ config.SchemaConfig, _ []*LazyChunk) error {
		close(fetching) // a single chunk means fetch runs exactly once
		<-ctx.Done()    // block until the query is canceled
		return ctx.Err()
	}

	chunks := []*LazyChunk{newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3))}
	it, err := newStreamFirstSampleBatchIterator(
		ctx, schemaConfig, metrics, chunks, 2, newMatchers(`{foo=~".+"}`),
		time.Unix(0, 0), time.Unix(0, 100*int64(time.Millisecond)), nil, 1, fetch, ex, iter.HintTimeRanges{})
	require.NoError(t, err)

	go func() {
		<-fetching
		cancel()
	}()

	require.False(t, it.Next())
	require.ErrorIs(t, it.Err(), context.Canceled)
	require.NoError(t, it.Close())

	var m dto.Metric
	require.NoError(t, metrics.streamFirstConsumerWait.Write(&m))
	require.Zero(t, m.GetHistogram().GetSampleCount(), "a canceled wait must not be observed")
}

// TestLazyStreamFirstSampleIterator_ReleasesConsumedStreams verifies the consumer decodes a
// stream only once fetched, and releases its Data once consumed.
func TestLazyStreamFirstSampleIterator_ReleasesConsumedStreams(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	fx := newStreamFirstPrefetchTestFixture(t)

	// Mark every chunk unfetched; only the injected fetch may validate them, so a stream decoded
	// before it's fetched would yield nothing.
	for _, c := range fx.chunks {
		c.IsValid = false
	}

	var fetchedChunks int
	fetch := func(_ context.Context, _ config.SchemaConfig, chunks []*LazyChunk) error {
		for _, c := range chunks {
			c.IsValid = true // simulate fetchLazyChunks validating the chunk
			fetchedChunks++
		}
		return nil
	}

	// batchSize 2 forces several batches so streams split across batch boundaries.
	rawIt, err := newStreamFirstSampleBatchIterator(
		context.Background(), fx.schema, NilMetrics, fx.chunks, 2,
		fx.matchers, fx.start, fx.end, nil, 0, fetch, fx.newEx(), iter.HintTimeRanges{})
	require.NoError(t, err)
	it, ok := rawIt.(*lazyStreamFirstSampleIterator)
	require.True(t, ok, "test relies on the concrete type to inspect per-stream release timing")

	// Snapshot each stream's chunks before draining: releaseStream nils it.streamChunks[idx] in
	// place.
	chunksBeforeRelease := make([][]*LazyChunk, len(it.streamChunks))
	copy(chunksBeforeRelease, it.streamChunks)

	var n, lastIdx int
	for it.Next() {
		_ = it.At()
		n++

		// Check every earlier stream is released as soon as the consumer moves on, not just once
		// the whole query drains.
		if it.idx > lastIdx {
			for i := lastIdx; i < it.idx; i++ {
				for _, c := range chunksBeforeRelease[i] {
					require.Nil(t, c.Chunk.Data, "stream %d must be released before stream %d starts", i, it.idx)
				}
			}
			lastIdx = it.idx
		}
	}
	require.NoError(t, it.Err())
	require.NoError(t, it.Close())

	require.Positive(t, n, "expected samples; got none (chunks likely decoded before fetch)")
	require.Equal(t, len(fx.chunks), fetchedChunks, "every chunk should be fetched exactly once")

	// The loop above never checks the last stream; verify it's released too.
	for _, c := range fx.chunks {
		require.Nil(t, c.Chunk.Data, "chunk Data should be released after its stream is consumed")
	}
}

func TestLazyStreamFirstSampleIterator_ClosePropagatesCurCloseError(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	batcher := newStreamFirstChunkBatcher(nil, 1)
	loader := newStreamFirstBatchLoader(config.SchemaConfig{}, NilMetrics, fetchLazyChunks)
	preloader := newStreamFirstChunkPreloader(context.Background(), batcher, loader, 1)

	closeBoom := errors.New("cur close failed")
	it := &lazyStreamFirstSampleIterator{
		preloader: preloader,
		cur:       fakeSampleCloser{SampleIterator: iter.NoopSampleIterator, closeErr: closeBoom},
	}

	// it.err is nil, yet Close must still surface cur's own close error.
	require.ErrorIs(t, it.Close(), closeBoom)
	require.NoError(t, it.Err(), "a close-time error must not leak into Err")
}

// streamFirstPrefetchTestFixture builds a small multi-stream chunk set, with the schema,
// matchers, and extractor to iterate it.
type streamFirstPrefetchTestFixture struct {
	schema     config.SchemaConfig
	chunks     []*LazyChunk
	matchers   []*labels.Matcher
	start, end time.Time
	newEx      func() log.SampleExtractor
}

func newStreamFirstPrefetchTestFixture(t *testing.T) streamFirstPrefetchTestFixture {
	t.Helper()

	periodConfig := config.PeriodConfig{From: config.DayTime{Time: 0}, Schema: "v11"}
	schemaConfig := config.SchemaConfig{Configs: []config.PeriodConfig{periodConfig}}
	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	// Multiple streams, each with several chunks, so batches span stream boundaries.
	var chunks []*LazyChunk
	for _, foo := range []string{"a", "b", "c"} {
		for c := 0; c < 3; c++ {
			base := int64(c*10 + 1)
			chunks = append(chunks, newLazyChunk(chunkfmt, headfmt, mkStream(foo, base, base+1, base+2)))
		}
	}

	return streamFirstPrefetchTestFixture{
		schema:   schemaConfig,
		chunks:   chunks,
		matchers: newMatchers(`{foo=~".+"}`),
		start:    time.Unix(0, 0),
		end:      time.Unix(0, 100*int64(time.Millisecond)),
		newEx: func() log.SampleExtractor {
			ex, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
			require.NoError(t, err)
			return ex
		},
	}
}

// fakeSampleCloser wraps a SampleIterator, overriding Close to return closeErr directly.
type fakeSampleCloser struct {
	iter.SampleIterator
	closeErr error

	// closeFunc, if set, runs instead of returning closeErr, to control Close's timing too.
	closeFunc func() error
}

func (f fakeSampleCloser) Close() error {
	if f.closeFunc != nil {
		return f.closeFunc()
	}
	return f.closeErr
}

// mkStream builds a single-series logproto.Stream labelled {foo="<fooVal>"} with one line per
// timestamp (in milliseconds); each line is distinct per timestamp.
func mkStream(fooVal string, tss ...int64) logproto.Stream {
	st := logproto.Stream{Labels: fmt.Sprintf(`{foo="%s"}`, fooVal)}
	for _, ts := range tss {
		st.Entries = append(st.Entries, logproto.Entry{
			Timestamp: time.Unix(0, ts*int64(time.Millisecond)),
			Line:      fmt.Sprintf("line-%d", ts),
		})
	}
	return st
}

// manyStreamsFixture builds streamCount distinct streams, each with chunksPerStream contiguous
// chunks of logsPerChunk lines: the shape a broad selector produces, dense enough that the
// batcher splits a stream's chunks across prefetch batches.
func manyStreamsFixture(streamCount, chunksPerStream, logsPerChunk int) []*logproto.Stream {
	streams := make([]*logproto.Stream, 0, streamCount*chunksPerStream)
	for i := 0; i < streamCount; i++ {
		for k := 0; k < chunksPerStream; k++ {
			entries := make([]logproto.Entry, logsPerChunk)
			for j := range entries {
				ts := int64(k*logsPerChunk+j) + 1 // contiguous across a stream's chunks
				entries[j] = logproto.Entry{Timestamp: time.Unix(0, ts), Line: "a very compressible log line duh"}
			}
			streams = append(streams, &logproto.Stream{Labels: fmt.Sprintf(`{foo="bar",id="%d"}`, i), Entries: entries})
		}
	}
	return streams
}
