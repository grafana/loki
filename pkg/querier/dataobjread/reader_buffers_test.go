package dataobjread

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/pkg/push"
)

const (
	// readerTestTimeout bounds each read, so a pool bug that hangs a scan fails the test instead
	// of the whole package. A full read of a fixture takes milliseconds.
	readerTestTimeout = 5 * time.Second

	// testConcurrency is how many sections the test readers scan at once.
	testConcurrency = 4

	// singleBuffer is the smallest pool that makes progress.
	singleBuffer = 1

	// smallBatch is a batch size that does not divide the rows of a stream, so the boundary
	// between two streams falls inside a batch.
	smallBatch = 7

	// manyBuffers is more buffers than a read of the fixture can use at once.
	manyBuffers = 64

	// memoryBoundFactor is how many times the memory of the first batch a single buffer may hold
	// after it served every batch of the fixture.
	memoryBoundFactor = 4
)

func TestBufferPool(t *testing.T) {
	t.Run("get returns a free buffer", func(t *testing.T) {
		pool := newBufferPool(2)

		buf, err := pool.get(t.Context())
		require.NoError(t, err)
		require.NotNil(t, buf)
	})

	t.Run("get blocks until a buffer is put back", func(t *testing.T) {
		pool := newBufferPool(1)
		held, err := pool.get(t.Context())
		require.NoError(t, err)

		type result struct {
			buf *scanBuffer
			err error
		}
		got := make(chan result, 1)
		go func() {
			buf, err := pool.get(t.Context())
			got <- result{buf, err}
		}()

		select {
		case <-got:
			t.Fatal("get returned while no buffer was free")
		case <-time.After(20 * time.Millisecond):
		}

		pool.put(held)
		select {
		case res := <-got:
			require.NoError(t, res.err)
			require.Same(t, held, res.buf)
		case <-time.After(readerTestTimeout):
			t.Fatal("get did not return after put")
		}
	})

	t.Run("get returns the error of the context when no buffer frees up", func(t *testing.T) {
		pool := newBufferPool(1)
		_, err := pool.get(t.Context())
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		got := make(chan error, 1)
		go func() {
			_, err := pool.get(ctx)
			got <- err
		}()

		select {
		case err := <-got:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(readerTestTimeout):
			t.Fatal("get ignored a cancelled context")
		}
	})

	t.Run("put drops what the buffer points to", func(t *testing.T) {
		pool := newBufferPool(1)
		buf, err := pool.get(t.Context())
		require.NoError(t, err)

		buf.setBatch(&logs.Batch{Rows: 1}, []streamRun{{end: 1, stream: objectStream{streamHash: 7}}}, true)
		pool.put(buf)

		buf, err = pool.get(t.Context())
		require.NoError(t, err)
		require.Nil(t, buf.batch)
		require.Nil(t, buf.records)
		require.Zero(t, buf.rows)
		require.Empty(t, buf.runs)
		require.Equal(t, streamRun{}, buf.runs[:1][0], "an idle buffer must not pin the label sets of its last runs")
	})

	t.Run("put panics when a buffer goes back twice", func(t *testing.T) {
		pool := newBufferPool(2)
		buf, err := pool.get(t.Context())
		require.NoError(t, err)
		_, err = pool.get(t.Context())
		require.NoError(t, err)
		pool.put(buf)

		require.PanicsWithValue(t, "dataobjread: buffer returned to the pool while it was free", func() { pool.put(buf) })
		require.Len(t, pool.free, 1)
	})

	t.Run("holds one buffer when it is asked for none", func(t *testing.T) {
		pool := newBufferPool(0)
		ctx, cancel := context.WithTimeout(t.Context(), readerTestTimeout)
		defer cancel()

		_, err := pool.get(ctx)
		require.NoError(t, err)
	})

	t.Run("poisons the message and metadata bytes of a batch when poisoning is on", func(t *testing.T) {
		defer poisonOnPut.Store(poisonOnPut.Swap(true))

		pool := newBufferPool(1)
		buf, err := pool.get(t.Context())
		require.NoError(t, err)

		batch := newRunsBatch([]int64{firstStreamID}, "line")
		batch.Metadata = []logs.MetadataColumn{{Name: "env", Values: newUTF8Column("prod")}}
		buf.setBatch(batch, nil, true)

		pool.put(buf)
		require.Equal(t, []byte{0xFF, 0xFF, 0xFF, 0xFF}, batch.Message.Data())
		require.Equal(t, []byte{0xFF, 0xFF, 0xFF, 0xFF}, batch.Metadata[0].Values.Data())
	})

	t.Run("release tolerates concurrent calls", func(t *testing.T) {
		pool := newBufferPool(3)
		for _, buf := range pool.all {
			_ = buf.alloc.Allocate(1 << 20)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				pool.release()
			}()
		}
		close(start)
		wg.Wait()
	})
}

func TestScanBuffer(t *testing.T) {
	t.Run("setBatch after setRecords leaves no records", func(t *testing.T) {
		buf := &scanBuffer{}
		buf.setRecords([]LogRecord{{timestamp: 1}, {timestamp: 2}})

		batch := newRunsBatch([]int64{firstStreamID})
		buf.setBatch(batch, []streamRun{{end: 1}}, true)

		require.Nil(t, buf.records)
		require.Same(t, batch, buf.batch)
		require.Equal(t, 1, buf.rows)
	})

	t.Run("setRecords after setBatch leaves no batch and no runs", func(t *testing.T) {
		buf := &scanBuffer{}
		buf.setBatch(newRunsBatch([]int64{firstStreamID}), []streamRun{{end: 1, stream: objectStream{streamHash: 7}}}, true)

		buf.setRecords([]LogRecord{{timestamp: 1}, {timestamp: 2}})

		require.Nil(t, buf.batch)
		require.Empty(t, buf.runs)
		require.Equal(t, streamRun{}, buf.runs[:1][0])
		require.Equal(t, 2, buf.rows)
	})

	t.Run("clear drops the runs that a failed read appended past the length", func(t *testing.T) {
		backing := make([]streamRun, 0, 4)
		_, err := newRunsTask(t).resolveRuns(newRunsBatch([]int64{firstStreamID, secondStreamID, 9999}), backing)
		require.Error(t, err)

		buf := &scanBuffer{runs: backing[:0]}
		buf.clear()

		require.Equal(t, streamRun{}, backing[:2][0])
		require.Equal(t, streamRun{}, backing[:2][1])
	})

	t.Run("reclaim keeps the allocator within a bound while the size of each read grows", func(t *testing.T) {
		buf := &scanBuffer{}

		var largest int
		for i := range 50 {
			buf.reclaim()
			largest = 1024 + 64*i
			_ = buf.alloc.Allocate(largest)
		}

		require.LessOrEqual(t, buf.alloc.AllocatedBytes(), 3*largest)
	})

	t.Run("reclaim reuses the regions that the last read used", func(t *testing.T) {
		buf := &scanBuffer{}
		buf.reclaim()
		_ = buf.alloc.Allocate(1024)
		first := buf.alloc.AllocatedBytes()

		for range 10 {
			buf.reclaim()
			_ = buf.alloc.Allocate(1024)
		}

		require.Equal(t, first, buf.alloc.AllocatedBytes())
	})
}

func TestNewLogReader(t *testing.T) {
	metrics := NewMetrics(nil)
	streams := newBufferTestStreams(t)

	queries := map[string]string{
		"a parsed line and all metadata":  `count_over_time({app=~".+"} | logfmt [1m])`,
		"a grouping by one metadata key":  `sum by (trace_id) (count_over_time({app=~".+"}[1m]))`,
		"no line and no metadata":         `sum(count_over_time({app=~".+"}[1m]))`,
		"a pushed line filter":            `count_over_time({app=~".+"} |= "level=error" [1m])`,
		"all metadata and the whole line": `bytes_over_time({app=~".+"}[1m])`,
	}
	for name, query := range queries {
		fixture := newBufferTestFixture(t, query, streams)

		for _, batchSize := range []int{1, smallBatch, DefaultReadBatchSize} {
			want := readAllRecords(t, fixture, metrics, false, singleBuffer, batchSize)
			require.NotEmpty(t, want)

			for _, bufferCount := range []int{singleBuffer, 2, 5} {
				t.Run(fmt.Sprintf("returns the rows of the row reader for %s with %d buffers and batches of %d", name, bufferCount, batchSize), func(t *testing.T) {
					require.Equal(t, want, readAllRecords(t, fixture, metrics, true, bufferCount, batchSize))
				})
			}
		}
	}

	fixture := newBufferTestFixture(t, `count_over_time({app=~".+"} | logfmt [1m])`, streams)

	t.Run("keeps the memory of a single buffer within a few batches while it serves every batch of a section", func(t *testing.T) {
		defer SetColumnarRead(SetColumnarRead(true))
		ctx, cancel := context.WithTimeout(t.Context(), readerTestTimeout)
		defer cancel()

		reader := newLogReader(ctx, fixture.objects, queuedTasks(fixture.tasks...), singleBuffer, 1, singleBuffer, metrics)
		require.True(t, reader.Next())
		first := reader.pool.all[0].alloc.AllocatedBytes()
		require.NotZero(t, first)

		for reader.Next() {
		}
		require.NoError(t, reader.Err())
		require.LessOrEqual(t, reader.pool.all[0].alloc.AllocatedBytes(), memoryBoundFactor*first)

		require.NoError(t, reader.Close())
		require.Zero(t, reader.pool.all[0].alloc.AllocatedBytes())
	})

	t.Run("gives every buffer back to the pool when a read ends", func(t *testing.T) {
		defer SetColumnarRead(SetColumnarRead(true))
		ctx, cancel := context.WithTimeout(t.Context(), readerTestTimeout)
		defer cancel()

		reader := newLogReader(ctx, fixture.objects, queuedTasks(fixture.tasks...), testConcurrency, smallBatch, manyBuffers, metrics)
		for reader.Next() {
		}
		require.NoError(t, reader.Err())

		require.Len(t, reader.pool.free, manyBuffers)
		require.NoError(t, reader.Close())
	})

	t.Run("fails the query when a batch holds a stream the task did not plan", func(t *testing.T) {
		defer SetColumnarRead(SetColumnarRead(true))
		ctx, cancel := context.WithTimeout(t.Context(), readerTestTimeout)
		defer cancel()

		tasks := slices.Clone(fixture.tasks)
		for i := range tasks {
			tasks[i].streams = newObjectStreams(map[int64]labels.Labels{}, func(labels.Labels, uint64) bool { return true })
		}

		reader := newLogReader(ctx, fixture.objects, queuedTasks(tasks...), testConcurrency, smallBatch, singleBuffer, metrics)
		for reader.Next() {
		}
		require.ErrorContains(t, reader.Err(), "unexpected stream ID")
		require.ErrorContains(t, reader.Close(), "unexpected stream ID")
	})

	t.Run("Close returns no error while scans wait for a buffer", func(t *testing.T) {
		defer SetColumnarRead(SetColumnarRead(true))

		reader := newLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), testConcurrency, 2, singleBuffer, metrics)
		require.True(t, reader.Next())
		_ = reader.At()

		requireClosesWithoutError(t, reader)
	})

	t.Run("Close returns no error before the first row is read", func(t *testing.T) {
		defer SetColumnarRead(SetColumnarRead(true))

		reader := newLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), testConcurrency, 2, singleBuffer, metrics)

		requireClosesWithoutError(t, reader)
	})
}

// newBufferTestStreams returns six streams of sixty lines. Every line has a trace ID and every
// third line has an environment, so rows have different metadata keys.
func newBufferTestStreams(t *testing.T) []logproto.Stream {
	t.Helper()

	var streams []logproto.Stream
	for s := range 6 {
		var entries []push.Entry
		for i := range 60 {
			metadata := []string{"trace_id", fmt.Sprintf("trace-%d", i%7)}
			if i%3 == 0 {
				metadata = append(metadata, "env", []string{"prod", "dev"}[i%2])
			}
			line := fmt.Sprintf("stream=%d line=%d level=%s", s, i, []string{"info", "error", "warn"}[i%3])
			entries = append(entries, entry(t, 1+i, line, metadata...))
		}
		streams = append(streams, logproto.Stream{Labels: fmt.Sprintf(`{app="a%d"}`, s), Entries: entries})
	}
	return streams
}

// newBufferTestFixture plans query over streams, with a section size that spreads the streams over
// several sections, so several scans compete for the buffers.
func newBufferTestFixture(t *testing.T, query string, streams []logproto.Stream) readerFixture {
	t.Helper()

	fixture := newReaderFixtureWithOptions(t, []objtest.Option{objtest.WithTargetSectionSize(2 * 1024)}, query, streams...)
	require.Greater(t, len(fixture.tasks), 3, "the fixture must have several sections")
	return fixture
}

// readAllRecords reads every record of the fixture and returns them as sorted strings. The
// consumer is slow on purpose, so batches wait in the channel while the scans want buffers.
func readAllRecords(t *testing.T, fixture readerFixture, metrics *Metrics, columnar bool, bufferCount, batchSize int) []string {
	t.Helper()
	defer SetColumnarRead(SetColumnarRead(columnar))

	ctx, cancel := context.WithTimeout(t.Context(), readerTestTimeout)
	defer cancel()

	reader := newLogReader(ctx, fixture.objects, queuedTasks(fixture.tasks...), testConcurrency, batchSize, bufferCount, metrics)

	var out []string
	for reader.Next() {
		if len(out)%5 == 0 {
			time.Sleep(20 * time.Microsecond)
		}
		rec := reader.At()
		out = append(out, fmt.Sprintf("%d|%d|%s|%s|%s", rec.streamHash, rec.timestamp, rec.streamLabels, rec.metadata, rec.line))
	}
	require.NoError(t, reader.Err())
	require.NoError(t, reader.Close())
	slices.Sort(out)
	return out
}

// readAllSamples runs query through the sample iterator over a reader with a single buffer and
// small batches, and returns the samples as sorted strings.
func readAllSamples(t *testing.T, fixture readerFixture, metrics *Metrics, query string, columnar bool) []string {
	t.Helper()
	defer SetColumnarRead(SetColumnarRead(columnar))

	ctx, cancel := context.WithTimeout(t.Context(), readerTestTimeout)
	defer cancel()

	reader := newLogReader(ctx, fixture.objects, queuedTasks(fixture.tasks...), testConcurrency, smallBatch, singleBuffer, metrics)
	iterator := NewSampleIterator(reader, newTestExtractor(t, query))

	var out []string
	for iterator.Next() {
		sample := iterator.At()
		out = append(out, fmt.Sprintf("%s|%d|%v|%d", iterator.Labels(), sample.Timestamp, sample.Value, iterator.StreamHash()))
	}
	require.NoError(t, iterator.Err())
	require.NoError(t, iterator.Close())
	slices.Sort(out)
	return out
}

// newTestExtractor builds the extractor the way the store does: from the query without the line
// filters that the read applies.
func newTestExtractor(t *testing.T, query string) syntax.SampleExtractor {
	t.Helper()

	expr, err := syntax.ParseSampleExpr(query)
	require.NoError(t, err)
	projection, err := NewProjectionPlan(expr, nil)
	require.NoError(t, err)
	if unpushed := projection.UnpushedExpr(); unpushed != nil {
		expr = unpushed
	}

	extractor, err := expr.Extractor()
	require.NoError(t, err)
	return extractor
}

func requireClosesWithoutError(t *testing.T, reader *LogReader) {
	t.Helper()

	closed := make(chan error, 1)
	go func() { closed <- reader.Close() }()

	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(readerTestTimeout):
		t.Fatal("Close did not return")
	}
}
