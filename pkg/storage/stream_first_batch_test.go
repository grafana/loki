package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

// TestSelectSamplesStreamFirstMatchesTimestampFirst drives the full store SelectSamples path — real
// fetcher, chunk cache, Data==nil ref chunks — with many streams, exactly the production shape. The
// stream-first order must return the same number of samples as timestamp-first; a silent chunk skip
// shows as fewer samples.
func TestSelectSamplesStreamFirstMatchesTimestampFirst(t *testing.T) {
	periodConfig := config.PeriodConfig{From: config.DayTime{Time: 0}, Schema: "v11"}
	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	const (
		streamCount     = 150
		chunksPerStream = 3
		logsPerChunk    = 5
	)

	var (
		streams      []*logproto.Stream
		totalSamples = streamCount * chunksPerStream * logsPerChunk
		query        = `count_over_time({foo=~".+"}[1m])`
		start, end   = time.Unix(0, 0), time.Unix(0, int64(chunksPerStream*logsPerChunk+1))
	)

	// Many fingerprints, each spanning several contiguous chunks (the shape a broad selector produces),
	// so a stream's chunks straddle prefetch batches and the batcher splits streams.
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

	selectSamples := func(t *testing.T, order logproto.SampleOrder, shards []astmapper.ShardAnnotation) int {
		st := &LokiStore{
			chunkMetrics: NilMetrics,
			cfg:          Config{MaxChunkBatchSize: 50},
			Store:        newMockChunkStore(chunkfmt, headfmt, streams),
		}
		_, ctx := stats.NewContext(user.InjectOrgID(context.Background(), "fake"))
		req := newSampleQuery(query, start, end, shards, nil)
		req.Order = order
		it, err := st.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: req})
		require.NoError(t, err)
		var n int
		for it.Next() {
			n++
		}
		require.NoError(t, it.Err())
		require.NoError(t, it.Close())
		return n
	}

	tsSamples := selectSamples(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, nil)
	sfSamples := selectSamples(t, logproto.SAMPLE_ORDER_BY_STREAM, nil)

	require.Equal(t, totalSamples, tsSamples, "sanity: timestamp-first must read every sample")
	require.Equal(t, tsSamples, sfSamples, "stream-first dropped samples")

	// A sharded request injects a __cortex_shard__ matcher (via req.Shards). The reader must strip it
	// before filtering series by matchers — no chunk carries that label — so a shard must not change
	// the sample count relative to the unsharded run for either order.
	t.Run("with a shard annotation", func(t *testing.T) {
		shard := []astmapper.ShardAnnotation{{Shard: 0, Of: 1}} // 0_of_1 selects everything
		tsSharded := selectSamples(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, shard)
		sfSharded := selectSamples(t, logproto.SAMPLE_ORDER_BY_STREAM, shard)
		require.Equal(t, totalSamples, tsSharded, "sanity: sharded timestamp-first must read every sample")
		require.Equal(t, totalSamples, sfSharded, "sharded stream-first dropped samples (shard matcher not stripped?)")
	})

	// The querier feeds the store iterator into a cross-source merge alongside other sources. Exercise
	// that multi-iterator merge (two disjoint stores) so its Outer/dedup loop is hit, not the single
	// iterator shortcut.
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

func TestNewStreamFirstSampleBatchIterator(t *testing.T) {
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

	// streamFirst builds the stream-first iterator over chunks with the given batching and fetch.
	streamFirst := func(ctx context.Context, chunks []*LazyChunk, batchSize, maxConcurrent int, fetch chunkFetchFunc) (iter.SampleIterator, error) {
		return newStreamFirstSampleBatchIterator(ctx, schemaConfig, NilMetrics, chunks, batchSize, matchers, start, end, nil, maxConcurrent, fetch, newEx())
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

	// millis turns millisecond values into the nanosecond timestamps the iterator returns.
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

		timestampFirstIterator, err := newTimestampFirstSampleBatchIterator(context.Background(), schemaConfig, NilMetrics, buildChunks(), 10, matchers, start, end, nil, newEx())
		require.NoError(t, err)
		streamFirstIterator, err := streamFirst(context.Background(), buildChunks(), 10, 0, fetchLazyChunks)
		require.NoError(t, err)

		timestampFirstEntries := drain(timestampFirstIterator)
		streamFirstEntries := drain(streamFirstIterator)

		// Same deduplicated data, regardless of order. The streamHash intentionally differs between
		// the two paths — the timestamp path exposes the extractor's reduced hash, the stream path
		// exposes the raw stream fingerprint the cross-source merge aligns on — so compare only
		// (labels, ts, value).
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

	t.Run("tracks decompressed bytes and lines like the timestamp-first iterator", func(t *testing.T) {
		// The stream-first iterator delegates per-stream decoding to the timestamp-first iterator on
		// the query context, so reordering streams must not change what the query decompresses. Feed
		// both paths the same chunks and require the store chunk stats to match, and to be non-zero so
		// a path that silently records nothing fails. Head-chunk bytes stay zero: the store reads
		// flushed (compressed) chunks, so those bytes are an ingester concern, not a store one.
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
			return newTimestampFirstSampleBatchIterator(ctx, schemaConfig, NilMetrics, buildChunks(), 10, matchers, start, end, nil, newEx())
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
		// [1,2,3], [3,4,5] (overlaps at ts 3 — a duplicate line) and a full duplicate of [1,2,3]; the
		// deduplicated stream is ts 1..5. batchSize 2 resolves the overlap across a batch boundary.
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
		// batchSize exceeds the chunk count → a single prefetch batch.
		it, err := streamFirst(context.Background(), chunks, 10, 1, fetchLazyChunks)
		require.NoError(t, err)
		require.Equal(t, millisToNanos(1, 2, 3, 4, 5, 6), drainTimestamps(t, it))
	})

	t.Run("returns the context error when canceled while waiting for the preloader", func(t *testing.T) {
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

	t.Run("surfaces a preloader fetch error and stops iteration", func(t *testing.T) {
		mockErr := errors.New("fetch failed")
		fetch := func(context.Context, config.SchemaConfig, []*LazyChunk) error { return mockErr }

		chunks := []*LazyChunk{newLazyChunk(chunkfmt, headfmt, mkStream("a", 1, 2, 3))}
		it, err := streamFirst(context.Background(), chunks, 2, 1, fetch)
		require.NoError(t, err)

		require.False(t, it.Next())
		require.ErrorIs(t, it.Err(), mockErr)
		require.NoError(t, it.Close())
	})
}

// streamFirstPrefetchTestFixture builds a small multi-stream chunk set plus the schema/matchers/extractor
// needed to iterate it, for the consumer-level prefetch tests below.
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

	// Multiple streams, each with several chunks, so batches split streams and span boundaries.
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

// TestLazyStreamFirstSampleIterator verifies the consumer only decodes a stream after the preloader
// has fetched its chunks, and frees each stream's compressed Data once the stream is consumed.
func TestLazyStreamFirstSampleIterator(t *testing.T) {
	fx := newStreamFirstPrefetchTestFixture(t)

	// Mark every chunk unfetched; the injected fetch is the only thing that may validate them.
	// If the consumer decoded a stream before it was fetched, buildHeapIterator would skip its
	// (invalid) chunks and the stream would yield nothing.
	for _, c := range fx.chunks {
		c.IsValid = false
	}

	var fetchedChunks int
	fetch := func(_ context.Context, _ config.SchemaConfig, chunks []*LazyChunk) error {
		for _, c := range chunks {
			require.NotNil(t, c.Chunk.Data, "fetch must run before Data is released")
			c.IsValid = true // simulate fetchLazyChunks validating the chunk
			fetchedChunks++
		}
		return nil
	}

	// batchSize 2 forces several batches so streams split across batch boundaries.
	it, err := newStreamFirstSampleBatchIterator(
		context.Background(), fx.schema, NilMetrics, fx.chunks, 2,
		fx.matchers, fx.start, fx.end, nil, 0, fetch, fx.newEx())
	require.NoError(t, err)

	var n int
	for it.Next() {
		_ = it.At()
		n++
	}
	require.NoError(t, it.Err())
	require.NoError(t, it.Close())

	require.Positive(t, n, "expected samples; got none (chunks likely decoded before fetch)")
	require.Equal(t, len(fx.chunks), fetchedChunks, "every chunk should be fetched exactly once")

	// After a full drain, every stream's compressed Data is released.
	for _, c := range fx.chunks {
		require.Nil(t, c.Chunk.Data, "chunk Data should be released after its stream is consumed")
	}
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
