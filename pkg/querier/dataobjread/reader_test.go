package dataobjread

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"

	"github.com/grafana/loki/pkg/push"
)

func TestLogReader(t *testing.T) {
	metrics := NewMetrics(nil)

	// bytes_over_time projects the message column, so a scan returns lines.
	const queryReturningLine = `bytes_over_time({app=~".+"}[1m])`

	t.Run("it yields every row of every planned section", func(t *testing.T) {
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

	t.Run("a batch size of one recycles batches and still yields every distinct line", func(t *testing.T) {
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
			t.Run(fmt.Sprintf("at batch size %d it yields every distinct line", batchSize), func(t *testing.T) {
				reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, batchSize, metrics)
				lines := drainReader(reader)
				require.NoError(t, reader.Err())
				require.NoError(t, reader.Close())
				require.ElementsMatch(t, want, lines)
			})
		}
	})

	t.Run("a long read reuses batches", func(t *testing.T) {
		var (
			streams []logproto.Stream
			lines   int
		)
		for s := range 4 {
			stream := logproto.Stream{Labels: fmt.Sprintf(`{app="app-%d"}`, s)}
			for i := range 25 {
				stream.Entries = append(stream.Entries, entry(t, i+1, fmt.Sprintf("stream-%d-line-%d", s, i)))
				lines++
			}
			streams = append(streams, stream)
		}
		smallSections := []objtest.Option{objtest.WithTargetSectionSize(1 << 10)}
		fixture := planReaderFixture(t, queryReturningLine, newObjectsFixtureWithOptions(t, smallSections, streams...))
		require.Greater(t, len(fixture.tasks), 1)

		const maxConcurrency = 1
		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), maxConcurrency, 1, metrics)
		batches := make(map[*rowsBatch]struct{})
		var read int
		for batch := reader.nextRowsBatch(); batch != nil; batch = reader.nextRowsBatch() {
			batches[batch] = struct{}{}
			read += batch.records.Rows
			batch.release()
		}
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
		require.Equal(t, lines, read)
		require.Less(t, len(batches), read/2)
	})

	t.Run("closing a reader drops the queued batches and keeps the batch that the consumer holds", func(t *testing.T) {
		var entries []push.Entry
		for i := range 10 {
			entries = append(entries, entry(t, i+1, fmt.Sprintf("line-%d", i)))
		}
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: entries})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), 4, 1, metrics)
		held := reader.nextRowsBatch()
		require.NotNil(t, held)
		require.Eventually(t, func() bool { return len(reader.batches) == cap(reader.batches) }, 10*time.Second, time.Millisecond)

		require.NoError(t, reader.Close())
		require.Empty(t, reader.batches)

		require.Nil(t, reader.nextRowsBatch())

		require.False(t, held.pooled)
		held.release()
		require.True(t, held.pooled)
	})

	t.Run("a reader overwrites the line of a real batch at its release in a test", func(t *testing.T) {
		skipWithoutPoisoning(t)
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one line")}})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), 1, 1, metrics)
		batch := reader.nextRowsBatch()
		require.NotNil(t, batch)
		line := batch.line(0)
		require.Equal(t, "one line", string(line))

		<-reader.stopped
		batch.release()
		for i, b := range line {
			require.EqualValues(t, 0xA5, b, "byte %d", i)
		}
		require.NoError(t, reader.Close())
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
			for batch := reader.nextRowsBatch(); batch != nil; batch = reader.nextRowsBatch() {
				for i := range batch.records.Rows {
					timestamps = append(timestamps, batch.records.Timestamp.Get(i))
				}
				batch.release()
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

	t.Run("closing before the rows are drained reports no error", func(t *testing.T) {
		entries := []push.Entry{
			entry(t, 1, "one"), entry(t, 2, "two"), entry(t, 3, "three"),
			entry(t, 4, "four"), entry(t, 5, "five"),
		}
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: entries})

		// One batch per row and a batch channel of one, so the scan is certainly blocked on a
		// send when Close cancels it. At the default batch size the scan finishes first and
		// reports nothing, so the suppression this asserts is never reached.
		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), 1, 1, metrics)
		first := reader.nextRowsBatch()
		require.NotNil(t, first)

		require.NoError(t, reader.Close(), "the cancellation Close causes is not a query failure")

		// The read really was cut short, so a cancellation was reported and suppressed rather
		// than never raised.
		received := first.records.Rows
		first.release()
		for batch := reader.nextRowsBatch(); batch != nil; batch = reader.nextRowsBatch() {
			received += batch.records.Rows
			batch.release()
		}
		require.Less(t, received, len(entries), "the scan finished, so nothing was suppressed")
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

		// A task with no decoded streams makes the scan dereference a nil pointer as it looks up
		// the stream of a run, which is a panic on the scan's own goroutine.
		task := fixture.tasks[0]
		task.streams = nil

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(task), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))

		require.ErrorContains(t, reader.Err(), "runtime error")
		require.Error(t, reader.Close())
	})

	t.Run("a read that exhausted its plan keeps every row a later cancellation finds queued", func(t *testing.T) {
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

		// Wait for the scan to finish, so every row is decoded and queued, then cancel.
		// Dropping the queued batches would under-count a query that read all it planned to.
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

	t.Run("a row of a stream that the task did not plan fails the read", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})
		task := fixture.tasks[0]
		task.streams = newObjectStreams(nil, func(labels.Labels, uint64) bool { return true })

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(task), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))
		require.ErrorContains(t, reader.Err(), "unexpected stream ID")
		require.Error(t, reader.Close())
	})

	t.Run("a task that projects no column of the section fails the read", func(t *testing.T) {
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}})
		task := fixture.tasks[0]
		task.columns = nil
		task.metadataNames = []string{"missing"}

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(task), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))
		require.ErrorContains(t, reader.Err(), "none of the projected columns are present in the section")
		require.Error(t, reader.Close())
	})

	t.Run("it sends batches of at most batchSize records", func(t *testing.T) {
		var entries []push.Entry
		for i := range 350 {
			entries = append(entries, entry(t, i%90+1, fmt.Sprintf("line-%d", i)))
		}
		fixture := newReaderFixture(t, `bytes_over_time({app=~".+"}[1m])`, logproto.Stream{Labels: `{app="a"}`, Entries: entries})

		const batchSize = 100
		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), 1, batchSize, NewMetrics(nil))

		var records int
		for batch := reader.nextRowsBatch(); batch != nil; batch = reader.nextRowsBatch() {
			records += batch.records.Rows
			require.LessOrEqual(t, batch.records.Rows, batchSize)
			batch.release()
		}
		require.NoError(t, reader.Close())
		require.Equal(t, len(entries), records)
	})

	t.Run("a line filter that returns an index out of range fails the read", func(t *testing.T) {
		fixture := newReaderFixture(t, `bytes_over_time({app=~".+"}[1m])`,
			logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one"), entry(t, 2, "two")}})
		task := fixture.tasks[0]
		task.predicates = []logs.RowPredicate{logs.LogMessageFilterRowPredicate{
			Keep:      func([]byte) bool { return true },
			KeepBatch: func(_ []byte, _ []int32, dst []int32) []int32 { return append(dst, 1000) },
		}}

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(task), DefaultMaxConcurrency, DefaultReadBatchSize, NewMetrics(nil))
		require.Empty(t, drainReader(reader))
		require.ErrorContains(t, reader.Err(), "kept index 1000")
		require.Error(t, reader.Close())
	})

	t.Run("a consumer that holds batches sees their lines intact while the scans go on", func(t *testing.T) {
		var entries []push.Entry
		for i := range 30 {
			entries = append(entries, entry(t, i+1, fmt.Sprintf("line-%d", i)))
		}
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: entries})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), 4, 1, metrics)

		var (
			held     []*rowsBatch
			heldWant [][]string
		)
		for range 3 {
			batch := reader.nextRowsBatch()
			require.NotNil(t, batch)
			held = append(held, batch)
			heldWant = append(heldWant, batchLines(batch))
		}
		rest := drainReader(reader)
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())

		require.Len(t, rest, len(entries)-len(held))
		for i, batch := range held {
			require.Equal(t, heldWant[i], batchLines(batch), "held batch %d", i)
			batch.release()
		}
	})

	t.Run("a read yields every line when released batches are overwritten", func(t *testing.T) {
		var (
			streams []logproto.Stream
			want    []string
		)
		for s := range 4 {
			stream := logproto.Stream{Labels: fmt.Sprintf(`{app="app-%d"}`, s)}
			for i := range 40 {
				line := fmt.Sprintf("stream-%d-line-%d", s, i)
				stream.Entries = append(stream.Entries, entry(t, i+1, line))
				want = append(want, line)
			}
			streams = append(streams, stream)
		}
		smallSections := []objtest.Option{objtest.WithTargetSectionSize(1 << 10)}
		fixture := planReaderFixture(t, queryReturningLine, newObjectsFixtureWithOptions(t, smallSections, streams...))

		for _, batchSize := range []int{1, 7, DefaultReadBatchSize} {
			reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, batchSize, metrics)
			lines := drainReader(reader)
			require.NoError(t, reader.Err())
			require.NoError(t, reader.Close())
			require.ElementsMatch(t, want, lines, "batch size %d", batchSize)
		}
	})
}

func TestLogReader_SendBatches(t *testing.T) {
	const batchSize = 1

	entries := []push.Entry{entry(t, 1, "one"), entry(t, 2, "two"), entry(t, 3, "three")}
	fixture := newReaderFixture(t, `bytes_over_time({app=~".+"}[1m])`, logproto.Stream{Labels: `{app="a"}`, Entries: entries})
	require.Len(t, fixture.tasks, 1)
	task := fixture.tasks[0]

	t.Run("it sends one batch for each read, and releases the batch that meets the end of the section", func(t *testing.T) {
		reader := newTrackedReader(len(entries))

		require.NoError(t, reader.sendBatches(t.Context(), openRowReader(t, fixture, task), task, batchSize))
		require.Len(t, reader.batches, len(entries))

		require.Len(t, reader.created, len(entries)+1)
		for _, sent := range reader.created[:len(entries)] {
			require.False(t, sent.pooled)
		}
		require.True(t, reader.created[len(entries)].pooled)
	})

	t.Run("it releases the batch when the read fails", func(t *testing.T) {
		reader := newTrackedReader(len(entries))
		rowReader := openRowReader(t, fixture, task)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		require.ErrorIs(t, reader.sendBatches(ctx, rowReader, task, batchSize), context.Canceled)

		require.Len(t, reader.created, 1)
		require.True(t, reader.created[0].pooled)
		require.Empty(t, reader.batches)
	})

	t.Run("it releases the batch when it cannot split the records into runs", func(t *testing.T) {
		reader := newTrackedReader(len(entries))
		unplanned := task
		unplanned.streams = newObjectStreams(nil, func(labels.Labels, uint64) bool { return true })

		err := reader.sendBatches(t.Context(), openRowReader(t, fixture, task), unplanned, batchSize)

		require.ErrorContains(t, err, "unexpected stream ID")
		require.Len(t, reader.created, 1)
		require.True(t, reader.created[0].pooled)
		require.Empty(t, reader.batches)
	})

	t.Run("it releases the batch of a read that fails after a send, and not the batch that it sent", func(t *testing.T) {
		reader := newTrackedReader(len(entries))

		var reads int
		failing := task
		failing.predicates = []logs.RowPredicate{logs.LogMessageFilterRowPredicate{
			Keep: func([]byte) bool { return true },
			KeepBatch: func(_ []byte, offsets []int32, dst []int32) []int32 {
				reads++
				if reads == 2 {
					return append(dst, 1000)
				}
				for i := 0; i+1 < len(offsets); i++ {
					dst = append(dst, int32(i))
				}
				return dst
			},
		}}

		err := reader.sendBatches(t.Context(), openRowReader(t, fixture, failing), failing, batchSize)

		require.ErrorContains(t, err, "kept index 1000")
		require.Len(t, reader.created, 2)
		require.Len(t, reader.batches, 1)
		sent := <-reader.batches
		require.Same(t, reader.created[0], sent)
		require.False(t, sent.pooled)
		require.True(t, reader.created[1].pooled)
	})

	t.Run("it releases the batch when the context is cancelled during the send", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		t.Cleanup(cancel)

		cancelling := task
		cancelling.predicates = []logs.RowPredicate{cancelOnFirstRow(cancel)}
		reader := newTrackedReader(0)

		err := reader.sendBatches(ctx, openRowReader(t, fixture, cancelling), cancelling, batchSize)

		require.ErrorIs(t, err, context.Canceled)
		require.Len(t, reader.created, 1)
		require.True(t, reader.created[0].pooled)
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
	streams, lineBytes := benchmarkStreams(streamCount, entriesPerStream)
	fixtures := []struct {
		name    string
		objects objectsFixture
	}{
		{name: "sections=1", objects: newObjectsFixture(b, streams...)},
		{name: "sections=26", objects: newObjectsFixtureWithOptions(b, []objtest.Option{objtest.WithTargetSectionSize(1 << 20)}, streams...)},
	}

	for _, query := range []string{
		`sum(count_over_time({app=~".+"}[1m]))`,
		`sum(bytes_over_time({app=~".+"}[1m]))`,
		`sum(count_over_time({app=~".+"} |= "error" [1m]))`,
		`sum(count_over_time({app=~".+"} |= "level=" [1m]))`,
		`sum(count_over_time({app=~".+"} |= "trace_id=0000000000000fa0" [1m]))`,
		`sum(count_over_time({app=~".+"} |= "error" | logfmt | status="500" [1m]))`,
		`sum(count_over_time({app=~".+"} | detected_level="error" [1m]))`,
		`sum(count_over_time({app=~".+"} | trace_id="0000000000000000" [1m]))`,
		`count_over_time({app=~".+"}[1m])`,
	} {
		metrics := NewMetrics(nil)

		for _, fx := range fixtures {
			fixture := planReaderFixture(b, query, fx.objects)

			b.Run(fmt.Sprintf("query=%s/%s", query, fx.name), func(b *testing.B) {
				b.SetBytes(lineBytes)
				b.ReportAllocs()
				for b.Loop() {
					reader := NewLogReader(b.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
					for batch := reader.nextRowsBatch(); batch != nil; batch = reader.nextRowsBatch() {
						batch.release()
					}
					require.NoError(b, reader.Close())
				}
			})
		}
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

// drainReader collects every line the reader yields, in no fixed order. It releases each batch
// after it copies the lines.
func drainReader(r *LogReader) []string {
	var lines []string
	for batch := r.nextRowsBatch(); batch != nil; batch = r.nextRowsBatch() {
		lines = append(lines, batchLines(batch)...)
		batch.release()
	}
	return lines
}

// openRowReader returns a row reader that is open over the section of task, as a scan has it.
func openRowReader(t *testing.T, fixture readerFixture, task ReadTask) *logs.RowReader {
	t.Helper()

	object, err := fixture.objects.get(t.Context(), task.objectPath)
	require.NoError(t, err)
	section, err := object.logsSection(t.Context(), task.sectionIdx)
	require.NoError(t, err)

	reader := logs.NewRowReader(section)
	require.NoError(t, reader.SetProjectedColumns(task.columns, task.metadataNames))
	require.NoError(t, reader.MatchStreams(slices.Values(task.streamIDs)))
	require.NoError(t, reader.SetPredicates(task.rowPredicates()))
	require.NoError(t, reader.Open(t.Context()))
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	return reader
}

// batchLines returns a copy of the line of each row of batch.
func batchLines(batch *rowsBatch) []string {
	lines := make([]string, batch.records.Rows)
	for i := range lines {
		lines[i] = string(batch.records.Message.Get(i))
	}
	return lines
}

// benchmarkStreams returns the streams of a benchmark fixture, and the number of line bytes they
// hold. Every line carries three structured metadata labels.
func benchmarkStreams(streamCount, entriesPerStream int) ([]logproto.Stream, int64) {
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
				StructuredMetadata: push.LabelsAdapter{
					{Name: "pod", Value: fmt.Sprintf("pod-%d-%d", s, i/2500)},
					{Name: "trace_id", Value: fmt.Sprintf("%016x", uint64(i)*0x9e3779b97f4a7c15)},
					{Name: "detected_level", Value: levels[i%len(levels)]},
				},
			})
		}
		streams = append(streams, stream)
	}
	return streams, lineBytes
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

// cancelOnFirstRow returns a row predicate that keeps every row and calls cancel each time it tests
// a row. A read that uses it returns its rows after the cancellation, so the context is already
// cancelled when the scan sends the batch.
func cancelOnFirstRow(cancel context.CancelFunc) logs.RowPredicate {
	return logs.LogMessageFilterRowPredicate{
		Keep: func([]byte) bool {
			cancel()
			return true
		},
	}
}

// batchOf returns records with one record per line. Record i belongs to the stream streamIDs[i] and
// has the timestamp at(i+1).
func batchOf(t *testing.T, streamIDs []int64, lines []string) *logs.RecordBatch {
	t.Helper()
	require.Len(t, lines, len(streamIDs))

	alloc := memory.NewAllocator(nil)
	ids := columnar.NewNumberBuilder[int64](alloc)
	timestamps := columnar.NewNumberBuilder[int64](alloc)
	messages := columnar.NewUTF8Builder(alloc)
	for i, line := range lines {
		ids.AppendValue(streamIDs[i])
		timestamps.AppendValue(at(i + 1).UnixNano())
		messages.AppendValue([]byte(line))
	}

	return &logs.RecordBatch{
		Rows:      len(lines),
		StreamID:  ids.BuildArray().(*columnar.Number[int64]),
		Timestamp: timestamps.BuildArray().(*columnar.Number[int64]),
		Message:   messages.BuildArray().(*columnar.UTF8),
	}
}

// trackedReader is a LogReader that is not running. It records each batch that its pool creates,
// so a test can check whether sendBatches released or sent the batch.
type trackedReader struct {
	*LogReader
	created []*rowsBatch
}

// newTrackedReader returns a trackedReader whose channel holds capacity batches.
func newTrackedReader(capacity int) *trackedReader {
	reader := &trackedReader{LogReader: &LogReader{pool: newRowsBatchPool(), batches: make(chan *rowsBatch, capacity)}}

	newBatch := reader.pool.free.New
	reader.pool.free.New = func() any {
		batch := newBatch().(*rowsBatch)
		reader.created = append(reader.created, batch)
		return batch
	}
	return reader
}
