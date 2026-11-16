package dataobjread

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/xcap"

	"github.com/grafana/loki/pkg/push"
)

func TestLogReader(t *testing.T) {
	metrics := NewMetrics(nil)

	// bytes_over_time projects the message column, which these tests need: the line is the one
	// field the row reader hands back out of a reused buffer.
	const queryReturningLine = `bytes_over_time({app=~".+"}[1m])`

	t.Run("it yields every record of every planned section", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine,
			logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one"), entry(t, 2, "two")}},
			logproto.Stream{Labels: `{app="b"}`, Entries: []push.Entry{entry(t, 1, "three")}},
		)
		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)

		lines := drainReader(reader)
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
		require.ElementsMatch(t, []string{"one", "two", "three"}, lines)
	})

	t.Run("a batch size of one recycles row buffers and still yields every distinct line", func(t *testing.T) {
		var entries []push.Entry
		want := make([]string, 0, 10)
		for i := range 10 {
			line := fmt.Sprintf("line-%d", i)
			entries = append(entries, entry(t, i+1, line))
			want = append(want, line)
		}
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: entries})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), 1, 1, metrics)
		lines := drainReader(reader)
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
		require.ElementsMatch(t, want, lines)
	})

	t.Run("concurrent scans that share recycled batches yield every distinct line", func(t *testing.T) {
		var (
			streams []logproto.Stream
			want    []string
		)
		for s := range 8 {
			stream := logproto.Stream{Labels: fmt.Sprintf(`{app="app-%d"}`, s)}
			for i := range 50 {
				line := fmt.Sprintf("stream-%d-line-%d", s, i)
				stream.Entries = append(stream.Entries, entry(t, i+1, line))
				want = append(want, line)
			}
			streams = append(streams, stream)
		}
		smallSections := []objtest.Option{objtest.WithTargetSectionSize(1 << 10)}
		fixture := planReaderFixture(t, queryReturningLine, newObjectsFixtureWithOptions(t, smallSections, streams...))
		require.Greater(t, len(fixture.tasks), 1)

		for _, batchSize := range []int{1, 3, DefaultReadBatchSize} {
			t.Run(fmt.Sprintf("at batch size %d", batchSize), func(t *testing.T) {
				reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, batchSize, metrics)
				lines := drainReader(reader)
				require.NoError(t, reader.Err())
				require.NoError(t, reader.Close())
				require.ElementsMatch(t, want, lines)
			})
		}
	})

	t.Run("a reader that yields every record frees every batch", func(t *testing.T) {
		var entries []push.Entry
		for i := range 10 {
			entries = append(entries, entry(t, i+1, fmt.Sprintf("line-%d", i)))
		}
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: entries})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), 1, 1, metrics)
		require.Len(t, drainReader(reader), len(entries))
		require.Nil(t, reader.currBatch)
		require.Zero(t, len(reader.batchPool.free))
		require.NoError(t, reader.Close())
	})

	t.Run("closing a reader before it yields every record frees the queued and pooled batches", func(t *testing.T) {
		var entries []push.Entry
		for i := range 10 {
			entries = append(entries, entry(t, i+1, fmt.Sprintf("line-%d", i)))
		}
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: entries})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), 4, 1, metrics)
		require.True(t, reader.Next())
		require.NoError(t, reader.Close())
		require.Zero(t, len(reader.batches))
		require.Zero(t, len(reader.batchPool.free))

		require.False(t, reader.Next())
		require.Nil(t, reader.currBatch)
		require.Zero(t, len(reader.batchPool.free))
	})

	t.Run("a pushed line filter keeps only the rows that pass it at every batch size", func(t *testing.T) {
		var (
			entries []push.Entry
			want    []int64
		)
		for i := range 25 {
			line := fmt.Sprintf("line-%d", i)
			entries = append(entries, entry(t, i+1, line))
			if strings.Contains(line, "1") {
				want = append(want, at(i+1).UnixNano())
			}
		}
		require.NotEmpty(t, want)

		fixture := newReaderFixture(t, `count_over_time({app=~".+"} |= "1" [1m])`, logproto.Stream{Labels: `{app="a"}`, Entries: entries})
		for _, task := range fixture.tasks {
			require.False(t, task.needReturningLine)
		}

		for _, batchSize := range []int{1, 4, DefaultReadBatchSize} {
			reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, batchSize, metrics)

			var timestamps []int64
			for reader.Next() {
				timestamps = append(timestamps, reader.At().timestamp)
			}
			require.NoError(t, reader.Err())
			require.NoError(t, reader.Close())
			require.ElementsMatch(t, want, timestamps, "batch size %d", batchSize)
		}
	})

	t.Run("a task naming a section the object does not hold fails the read", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})

		task := fixture.tasks[0]
		task.sectionIdx = 999

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(task), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))
		require.ErrorContains(t, reader.Err(), "holds no logs section 999")
		require.Error(t, reader.Close())
	})

	t.Run("a task naming an object the bucket does not hold fails the read", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})

		task := fixture.tasks[0]
		task.objectPath = "objects/does-not-exist"

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(task), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))
		require.Error(t, reader.Err())
		require.Error(t, reader.Close())
	})

	t.Run("one failing scan does not stop the reader reporting the failure", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine,
			logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}},
			logproto.Stream{Labels: `{app="b"}`, Entries: []push.Entry{entry(t, 1, "two")}},
		)

		broken := fixture.tasks[0]
		broken.sectionIdx = 999
		tasks := append([]ReadTask{broken}, fixture.tasks...)

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		drainReader(reader)
		require.Error(t, reader.Err(), "a failed scan must not be reported as success")
		require.Error(t, reader.Close())
	})

	t.Run("a cancelled context is reported rather than looking like a clean end", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		reader := NewLogReader(ctx, fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		drainReader(reader)
		require.ErrorIs(t, reader.Err(), context.Canceled, "a cancelled query must not return a truncated result with no error")
	})

	t.Run("a planning error surfaces even when no task was planned", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})

		tasks := queuedTasks()
		wantErr := errors.New("resolution failed")
		tasks.setErr(wantErr)

		reader := NewLogReader(t.Context(), fixture.objects, tasks, DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))
		require.ErrorIs(t, reader.Err(), wantErr)
	})

	t.Run("closing before the records are drained reports no error", func(t *testing.T) {
		entries := []push.Entry{
			entry(t, 1, "one"), entry(t, 2, "two"), entry(t, 3, "three"),
			entry(t, 4, "four"), entry(t, 5, "five"),
		}
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: entries})

		// One batch per record and a batch channel of one, so the scan is certainly blocked on a
		// send when Close cancels it. At the default batch size the scan finishes first and
		// reports nothing, so the suppression this asserts is never reached.
		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), 1, 1, metrics)
		require.True(t, reader.Next())

		require.NoError(t, reader.Close(), "the cancellation Close causes is not a query failure")

		// The read really was cut short, so a cancellation was reported and suppressed rather
		// than never raised. Counting is all this does with the records: Close released the
		// objects they were decoded from.
		forwarded := 1
		for reader.Next() {
			forwarded++
		}
		require.Less(t, forwarded, len(entries), "the scan finished, so nothing was suppressed")
	})

	t.Run("a cancellation the caller caused is still reported after Close", func(t *testing.T) {
		// Close suppresses the cancellation it causes itself. It must not suppress one the
		// caller caused, even when no scan has reported it yet: the query did fail, and a
		// truncated result would otherwise look authoritative.
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{
			Labels:  `{app="a"}`,
			Entries: []push.Entry{entry(t, 1, "one"), entry(t, 2, "two")},
		})

		ctx, cancel := context.WithCancel(t.Context())
		reader := NewLogReader(ctx, fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		cancel()

		require.ErrorIs(t, reader.Close(), context.Canceled)
		require.ErrorIs(t, reader.Err(), context.Canceled)
	})

	t.Run("a planning cancellation the reader did not cause still fails the read", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})

		// The caller's context is alive and the reader has stopped nothing, so a cancellation
		// arriving from the planner is somebody else's, and a real failure.
		tasks := queuedTasks()
		tasks.setErr(context.Canceled)

		reader := NewLogReader(t.Context(), fixture.objects, tasks, DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))

		require.ErrorIs(t, reader.Err(), context.Canceled)
		require.ErrorIs(t, reader.Close(), context.Canceled)
	})

	t.Run("the caller's own cause is reported, not a bare cancellation", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})

		ctx, cancel := context.WithCancelCause(t.Context())
		cancel(errors.New("query timed out upstream"))

		reader := NewLogReader(ctx, fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		drainReader(reader)

		require.ErrorContains(t, reader.Err(), "query timed out upstream", "the cause says why, where the bare cancellation does not")
	})

	t.Run("the first failure reported is the one kept", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))

		// A scan cut short by a sibling's failure reports its own cancellation afterwards. The
		// first error is the one that explains the rest.
		reader.setErr(errors.New("first"))
		reader.setErr(errors.New("second"))
		require.EqualError(t, reader.Err(), "first")
	})

	t.Run("a panic scanning a section fails the read rather than the process", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})

		// A task with no decoded streams makes the scan dereference a nil pointer as it turns
		// rows into records, which is a panic on the scan's own goroutine.
		task := fixture.tasks[0]
		task.streams = nil

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(task), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))

		require.ErrorContains(t, reader.Err(), "runtime error")
		require.Error(t, reader.Close())
	})

	t.Run("a read that exhausted its plan keeps every record a later cancellation finds queued", func(t *testing.T) {
		var entries []push.Entry
		want := make([]string, 0, 20)
		for i := range 20 {
			line := fmt.Sprintf("line-%d", i)
			entries = append(entries, entry(t, i+1, line))
			want = append(want, line)
		}
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: entries})

		ctx, cancel := context.WithCancel(t.Context())
		reader := NewLogReader(ctx, fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)

		// Wait for the scan to finish, so every record is decoded and queued, then cancel.
		// Dropping the queued records would under-count a query that read all it planned to.
		<-reader.stopped
		cancel()

		require.ElementsMatch(t, want, drainReader(reader))
		require.NoError(t, reader.Err())
	})

	t.Run("closing twice reports no error the second time", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		drainReader(reader)
		require.NoError(t, reader.Close())
		require.NoError(t, reader.Close())
	})

	t.Run("a plan with no task yields nothing and reports no error", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
	})

	t.Run("Close reports the slowest metastore section resolution of the capture in the query stats", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})
		statsCtx, ctx := stats.NewContext(t.Context())
		ctx, _ = xcap.NewCapture(ctx, nil)
		for _, d := range []time.Duration{7 * time.Millisecond, 3 * time.Millisecond} {
			_, region := xcap.StartRegion(ctx, metastore.RegionSections)
			region.Record(metastore.StatMetastoreSectionsDuration.Observe(int64(d)))
		}

		reader := NewLogReader(ctx, fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		drainReader(reader)
		require.NoError(t, reader.Close())

		require.Equal(t, 7*time.Millisecond, statsCtx.Result(0, 0, 0).DataobjSectionsResolutionMaxTime())
	})

	t.Run("Close reports no section resolution time when the capture holds no metastore region", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})
		statsCtx, ctx := stats.NewContext(t.Context())
		ctx, _ = xcap.NewCapture(ctx, nil)

		reader := NewLogReader(ctx, fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		drainReader(reader)
		require.NoError(t, reader.Close())

		require.Zero(t, statsCtx.Result(0, 0, 0).DataobjSectionsResolutionMaxTime())
	})
}

func TestReadBatchPool(t *testing.T) {
	const batchSize = 4

	t.Run("get on an empty pool returns a batch with batchSize rows and no records", func(t *testing.T) {
		pool := newReadBatchPool(1, batchSize)

		batch := pool.get()
		require.Len(t, batch.rows, batchSize)
		require.Empty(t, batch.records)
		require.Equal(t, batchSize, cap(batch.records))
	})

	t.Run("get after put returns the same batch with its line buffers", func(t *testing.T) {
		pool := newReadBatchPool(1, batchSize)
		batch := pool.get()
		batch.rows[0].Line = append(batch.rows[0].Line, "line"...)
		lineBuffer := &batch.rows[0].Line[0]

		pool.put(batch)
		recycled := pool.get()
		require.Same(t, batch, recycled)
		require.Same(t, lineBuffer, &recycled.rows[0].Line[0])
	})

	t.Run("put drops the records, including those past their length, and the row metadata", func(t *testing.T) {
		pool := newReadBatchPool(1, batchSize)
		batch := pool.get()
		metadata := labels.FromStrings("level", "error")
		batch.rows[0].Metadata = metadata
		batch.records = append(batch.records,
			LogRecord{streamLabels: labels.FromStrings("app", "a"), metadata: metadata},
			LogRecord{streamLabels: labels.FromStrings("app", "b"), metadata: metadata},
		)
		batch.records = batch.records[:1]

		pool.put(batch)
		require.Empty(t, batch.records)
		for _, record := range batch.records[:cap(batch.records)] {
			require.Equal(t, LogRecord{}, record)
		}
		require.True(t, batch.rows[0].Metadata.IsEmpty())
	})

	t.Run("put drops a line buffer larger than the pooled limit and keeps a smaller one", func(t *testing.T) {
		pool := newReadBatchPool(1, batchSize)
		batch := pool.get()
		batch.rows[0].Line = make([]byte, maxPooledLineBytes+1)
		batch.rows[1].Line = make([]byte, maxPooledLineBytes)

		pool.put(batch)
		require.Nil(t, batch.rows[0].Line)
		require.Equal(t, maxPooledLineBytes, cap(batch.rows[1].Line))
	})

	t.Run("put panics when the batch is already in the pool", func(t *testing.T) {
		pool := newReadBatchPool(2, batchSize)
		batch := pool.get()
		pool.put(batch)

		require.PanicsWithValue(t, "dataobjread: read batch returned to the pool twice", func() { pool.put(batch) })
	})

	t.Run("put panics when the pool is full", func(t *testing.T) {
		pool := newReadBatchPool(1, batchSize)
		first, second := pool.get(), pool.get()
		pool.put(first)

		require.PanicsWithValue(t, "dataobjread: read batch pool is full, so a batch has more than one owner", func() { pool.put(second) })
	})

	t.Run("drain removes every batch from the pool", func(t *testing.T) {
		pool := newReadBatchPool(2, batchSize)
		first, second := pool.get(), pool.get()
		pool.put(first)
		pool.put(second)

		pool.drain()
		require.Zero(t, len(pool.free))
		require.NotSame(t, first, pool.get())
	})
}

// TestLogReader_ConcurrentObjectOpens drives the open-object registry the way the planner
// and the scans do: two goroutines asking for one path at once. They must share one open, so both
// callers get the same opened object and the path counts as one opened object.
func TestLogReader_ConcurrentObjectOpens(t *testing.T) {
	fixture := newObjectsFixture(t, logproto.Stream{
		Labels:  `{app="a"}`,
		Entries: []push.Entry{entry(t, 1, "one")},
	})
	path := fixture.descriptors[0].ObjectPath

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		objects := NewOpenObjects(&holdBucket{Bucket: fixture.bucket, release: release}, objtest.Tenant, DefaultHeadPrefetchBytes)
		ctx, capture := xcap.NewCapture(t.Context(), nil)
		ctx, _ = xcap.StartRegion(ctx, regionStreamsReader)

		const callers = 2
		opened := make([]*openObject, callers)
		errs := make([]error, callers)
		var wg sync.WaitGroup
		for i := range callers {
			wg.Go(func() { opened[i], errs[i] = objects.get(ctx, path) })
		}

		// Both callers are blocked now. One waits on the bucket, and the other waits on its open, unless
		// the registry lets each caller open the object itself.
		synctest.Wait()
		close(release)
		wg.Wait()

		for i := range callers {
			require.NoError(t, errs[i])
			require.Same(t, opened[0], opened[i], "every caller must share one opened object")
		}
		require.Equal(t, int64(1), xcap.Value[int64](capture, dataobj.StatObjectsOpenedUncompacted), "the callers must share one open")
	})
}

func BenchmarkLogReader(b *testing.B) {
	const (
		streamCount      = 20
		entriesPerStream = 10_000
	)
	levels := []string{"info", "warn", "error", "debug"}
	paths := []string{"/api/v1/push", "/loki/api/v1/query_range", "/metrics", "/ready"}

	var (
		streams   []logproto.Stream
		lineBytes int64
	)
	for s := range streamCount {
		stream := logproto.Stream{Labels: fmt.Sprintf(`{app="app-%d"}`, s)}
		for i := range entriesPerStream {
			line := fmt.Sprintf(`level=%s caller=http.go:%d msg="request completed" method=GET path=%s status=%d duration=%dms trace_id=%016x`,
				levels[i%len(levels)], i%500, paths[(i/7)%len(paths)], 200+(i%4)*100, (i*7919)%5000, uint64(i)*0x9e3779b97f4a7c15)
			lineBytes += int64(len(line))
			stream.Entries = append(stream.Entries, push.Entry{
				Timestamp: at(1).Add(time.Duration(i) * 5 * time.Millisecond),
				Line:      line,
			})
		}
		streams = append(streams, stream)
	}

	for _, query := range []string{
		`bytes_over_time({app=~".+"}[1m])`,
		`count_over_time({app=~".+"} |= "error" [1m])`,
		`count_over_time({app=~".+"} |= "error" | logfmt [1m])`,
	} {
		b.Run(query, func(b *testing.B) {
			fixture := newReaderFixture(b, query, streams...)
			metrics := NewMetrics(nil)

			b.SetBytes(lineBytes)
			b.ReportAllocs()
			for b.Loop() {
				reader := NewLogReader(b.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
				for reader.Next() {
					_ = reader.At()
				}
				require.NoError(b, reader.Close())
			}
		})
	}
}

// readerFixture is an open-object registry together with the real tasks a plan produced over it,
// so a reader test drives the same tasks production would.
type readerFixture struct {
	objects *OpenObjects
	tasks   []ReadTask
}

// newReaderFixture plans the read of query over streams.
func newReaderFixture(t testing.TB, query string, streams ...logproto.Stream) readerFixture {
	t.Helper()
	return planReaderFixture(t, query, newObjectsFixture(t, streams...))
}

// planReaderFixture plans the read of query over the objects of fixture.
func planReaderFixture(t testing.TB, query string, fixture objectsFixture) readerFixture {
	t.Helper()

	objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes)
	t.Cleanup(objects.release)

	expr, err := syntax.ParseSampleExpr(query)
	require.NoError(t, err)
	projection, err := NewProjectionPlan(expr, nil)
	require.NoError(t, err)

	readPlanner := NewPlanner(&fixedMetastore{descriptors: fixture.descriptors}, objects, nil)
	tasks, err := drainTasks(readPlanner.Plan(t.Context(), QueryParams{
		Start:      at(0),
		End:        at(100),
		Matchers:   syntax.MustParseLogSelector(`{app=~".+"}`, true).Matchers(),
		Projection: projection,
	}))
	require.NoError(t, err)
	require.NotEmpty(t, tasks)

	return readerFixture{objects: objects, tasks: tasks}
}

// drainReader collects every line the reader yields, as a set.
func drainReader(r *LogReader) []string {
	var lines []string
	for r.Next() {
		lines = append(lines, string(r.At().line))
	}
	return lines
}

// holdBucket holds each Attributes, Get and GetRange call until release closes, then passes the
// call through.
type holdBucket struct {
	objstore.Bucket

	release chan struct{}
}

func (b *holdBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	<-b.release
	return b.Bucket.Attributes(ctx, name)
}

func (b *holdBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	<-b.release
	return b.Bucket.Get(ctx, name)
}

func (b *holdBucket) GetRange(ctx context.Context, name string, offset, length int64) (io.ReadCloser, error) {
	<-b.release
	return b.Bucket.GetRange(ctx, name, offset, length)
}
