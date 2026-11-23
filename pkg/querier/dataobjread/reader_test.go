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

	// bytes_over_time projects the message column, which these tests need: the line is the one
	// field that a scan copies into recycled memory.
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
		batches := make(map[*readBatch]struct{})
		var read int
		for reader.Next() {
			batches[reader.currBatch] = struct{}{}
			read++
		}
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
		require.Equal(t, lines, read)
		require.Less(t, len(batches), read/2)
	})

	t.Run("a reader that yields every record holds no batch", func(t *testing.T) {
		var entries []push.Entry
		for i := range 10 {
			entries = append(entries, entry(t, i+1, fmt.Sprintf("line-%d", i)))
		}
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: entries})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), 1, 1, metrics)
		require.Len(t, drainReader(reader), len(entries))
		require.Nil(t, reader.currBatch)
		require.NoError(t, reader.Close())
	})

	t.Run("closing a reader before it yields every record drops the queued batches", func(t *testing.T) {
		var entries []push.Entry
		for i := range 10 {
			entries = append(entries, entry(t, i+1, fmt.Sprintf("line-%d", i)))
		}
		fixture := newReaderFixture(t, queryReturningLine, logproto.Stream{Labels: `{app="a"}`, Entries: entries})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), 4, 1, metrics)
		require.True(t, reader.Next())
		require.NoError(t, reader.Close())
		require.Empty(t, reader.batches)

		require.False(t, reader.Next())
		require.Nil(t, reader.currBatch)
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

	t.Run("it forwards batches of at most batchSize records", func(t *testing.T) {
		var entries []push.Entry
		for i := range 350 {
			entries = append(entries, entry(t, i%90+1, fmt.Sprintf("line-%d", i)))
		}
		fixture := newReaderFixture(t, `bytes_over_time({app=~".+"}[1m])`, logproto.Stream{Labels: `{app="a"}`, Entries: entries})

		const batchSize = 100
		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), 1, batchSize, NewMetrics(nil))

		var records int
		for reader.Next() {
			records++
			require.LessOrEqual(t, len(reader.currBatch.records), batchSize)
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

	t.Run("a scan reuses the memory of its allocator for every batch", func(t *testing.T) {
		var entries []push.Entry
		for i := range 200 {
			entries = append(entries, entry(t, i%90+1, fmt.Sprintf("line-%03d", i)))
		}
		fixture := newReaderFixture(t, `bytes_over_time({app=~".+"}[1m])`, logproto.Stream{Labels: `{app="a"}`, Entries: entries})
		require.Len(t, fixture.tasks, 1)
		task := fixture.tasks[0]

		// allocatedBytes returns the memory that the allocator of a scan holds after the scan reads
		// every record in batches of the given size.
		allocatedBytes := func(batchSize int) int {
			object, err := fixture.objects.get(t.Context(), task.objectPath)
			require.NoError(t, err)
			section, err := object.logsSection(t.Context(), task.sectionIdx)
			require.NoError(t, err)

			rowReader := logs.NewRowReader(section)
			require.NoError(t, rowReader.SetProjectedColumns(task.columns, task.metadataNames))
			require.NoError(t, rowReader.MatchStreams(slices.Values(task.streamIDs)))
			require.NoError(t, rowReader.SetPredicates(task.rowPredicates()))
			require.NoError(t, rowReader.Open(t.Context()))
			t.Cleanup(func() { require.NoError(t, rowReader.Close()) })

			reader := &LogReader{batchPool: newReadBatchPool(batchSize), batches: make(chan *readBatch, len(entries))}
			var alloc memory.Allocator
			batch, err := reader.sendBatches(t.Context(), rowReader, &alloc, task, batchSize)
			require.NoError(t, err)
			require.Nil(t, batch)
			return alloc.AllocatedBytes()
		}

		// A scan that did not reclaim between batches would hold far more memory with a batch size of
		// one than with one batch. Reclaiming keeps both close.
		require.LessOrEqual(t, allocatedBytes(1), 2*allocatedBytes(len(entries)))
	})
}

func TestReadBatch_Fill(t *testing.T) {
	const streamID = int64(7)
	task := lineTask(t, streamID)

	t.Run("builds a record for each row with the identity of its stream", func(t *testing.T) {
		in := batchOf(t, []int64{streamID, streamID}, []string{"one", "two"})
		levels := columnar.NewUTF8Builder(memory.NewAllocator(nil))
		levels.AppendValue([]byte("error"))
		levels.AppendValue([]byte(""))
		in.Metadata = append(in.Metadata, logs.MetadataColumn{Name: "level", Values: levels.BuildArray().(*columnar.UTF8)})

		var batch readBatch
		require.NoError(t, batch.fill(task, in))
		require.Len(t, batch.records, 2)

		stream := task.streams.admittedByID[streamID]
		for i, line := range []string{"one", "two"} {
			require.Equal(t, stream.streamHash, batch.records[i].streamHash)
			require.Equal(t, stream.streamLabels, batch.records[i].streamLabels)
			require.Equal(t, at(i+1).UnixNano(), batch.records[i].timestamp)
			require.Equal(t, line, string(batch.records[i].line))
		}
		require.Equal(t, `{level="error"}`, batch.records[0].metadata.String())
		require.Equal(t, `{level=""}`, batch.records[1].metadata.String())
	})

	t.Run("attaches the identity of the stream of each row when the streams interleave", func(t *testing.T) {
		streams := newObjectStreams(
			map[int64]labels.Labels{7: labels.FromStrings("app", "a"), 8: labels.FromStrings("app", "b")},
			func(labels.Labels, uint64) bool { return true },
		)
		interleaved := ReadTask{streamIDs: []int64{7, 8}, streams: streams, needReturningLine: true}

		ids := []int64{7, 8, 7, 8, 8}
		in := batchOf(t, ids, []string{"a", "b", "c", "d", "e"})

		var batch readBatch
		require.NoError(t, batch.fill(interleaved, in))
		require.Len(t, batch.records, len(ids))
		for i, id := range ids {
			require.Equal(t, streams.admittedByID[id].streamLabels, batch.records[i].streamLabels, "row %d", i)
			require.Equal(t, streams.admittedByID[id].streamHash, batch.records[i].streamHash, "row %d", i)
		}
	})

	t.Run("copies the lines into one region, so a later read does not change them", func(t *testing.T) {
		in := batchOf(t, []int64{streamID, streamID}, []string{"one", "two"})

		var batch readBatch
		require.NoError(t, batch.fill(task, in))

		copy(in.Message.(*columnar.UTF8).Data(), "xxxxxx")
		require.Equal(t, "one", string(batch.records[0].line))
		require.Equal(t, "two", string(batch.records[1].line))
	})

	t.Run("sizes the region of the lines from the lines of a sliced array", func(t *testing.T) {
		full := batchOf(t, []int64{streamID, streamID, streamID}, []string{"one", "two", "three"})
		sliced := &logs.RecordBatch{
			Rows:      2,
			StreamID:  full.StreamID.Slice(1, 3).(*columnar.Number[int64]),
			Timestamp: full.Timestamp.Slice(1, 3).(*columnar.Number[int64]),
			Message:   full.Message.Slice(1, 3).(*columnar.UTF8),
		}
		require.Equal(t, len("two")+len("three"), task.forwardedLineBytes(sliced))

		var batch readBatch
		require.NoError(t, batch.fill(task, sliced))
		require.Equal(t, "two", string(batch.records[0].line))
		require.Equal(t, "three", string(batch.records[1].line))
		require.Equal(t, len("two")+len("three"), batch.linesAlloc.AllocatedBytes())
	})

	t.Run("reads the lines of a message that is a view over several arrays", func(t *testing.T) {
		full := batchOf(t, []int64{streamID, streamID, streamID}, []string{"one", "two", "three"})
		first := batchOf(t, []int64{streamID}, []string{"one"}).Message.(*columnar.UTF8)
		second := batchOf(t, []int64{streamID, streamID}, []string{"two", "three"}).Message.(*columnar.UTF8)
		viewed := &logs.RecordBatch{
			Rows:      3,
			StreamID:  full.StreamID,
			Timestamp: full.Timestamp,
			Message:   columnar.NewChunkedUTF8([]*columnar.UTF8{first, second}),
		}
		require.Equal(t, len("one")+len("two")+len("three"), task.forwardedLineBytes(viewed))

		var batch readBatch
		require.NoError(t, batch.fill(task, viewed))
		require.Equal(t, "one", string(batch.records[0].line))
		require.Equal(t, "two", string(batch.records[1].line))
		require.Equal(t, "three", string(batch.records[2].line))
	})

	t.Run("a second fill reuses the region of the first", func(t *testing.T) {
		var batch readBatch
		require.NoError(t, batch.fill(task, batchOf(t, []int64{streamID}, []string{"first"})))
		allocated := batch.linesAlloc.AllocatedBytes()

		require.NoError(t, batch.fill(task, batchOf(t, []int64{streamID}, []string{"other"})))
		require.Equal(t, "other", string(batch.records[0].line))
		require.Equal(t, allocated, batch.linesAlloc.AllocatedBytes())
	})

	t.Run("allocates no line memory when the task does not need the line", func(t *testing.T) {
		noLine := task
		noLine.needReturningLine = false

		var batch readBatch
		require.NoError(t, batch.fill(noLine, batchOf(t, []int64{streamID}, []string{"one"})))
		require.Len(t, batch.records, 1)
		require.Nil(t, batch.records[0].line)
		require.Zero(t, batch.linesAlloc.AllocatedBytes())
	})

	t.Run("forwards no line for an empty line and a line for the others", func(t *testing.T) {
		var batch readBatch
		require.NoError(t, batch.fill(task, batchOf(t, []int64{streamID, streamID}, []string{"", "x"})))
		require.Nil(t, batch.records[0].line)
		require.Equal(t, "x", string(batch.records[1].line))
	})

	t.Run("caps each line at its own length, so an append does not overwrite the next line", func(t *testing.T) {
		var batch readBatch
		require.NoError(t, batch.fill(task, batchOf(t, []int64{streamID, streamID}, []string{"one", "two"})))

		first := batch.records[0].line
		require.Equal(t, len(first), cap(first))
		_ = append(first, "xxxx"...)
		require.Equal(t, "two", string(batch.records[1].line))
	})

	t.Run("forwards no line when the batch has no message column", func(t *testing.T) {
		in := batchOf(t, []int64{streamID}, []string{"one"})
		in.Message = nil

		var batch readBatch
		require.NoError(t, batch.fill(task, in))
		require.Nil(t, batch.records[0].line)
	})

	t.Run("fails and leaves no records for a row of a stream that the task did not plan", func(t *testing.T) {
		var batch readBatch
		require.NoError(t, batch.fill(task, batchOf(t, []int64{streamID}, []string{"earlier"})))

		err := batch.fill(task, batchOf(t, []int64{streamID, 9999}, []string{"one", "two"}))
		require.ErrorContains(t, err, "unexpected stream ID 9999")
		require.Empty(t, batch.records)
	})

	t.Run("fails for a batch without a stream ID array", func(t *testing.T) {
		in := batchOf(t, []int64{streamID}, []string{"one"})
		in.StreamID = nil

		var batch readBatch
		require.ErrorContains(t, batch.fill(task, in), "without the stream ID or timestamp column")
	})

	t.Run("fails for a batch without a timestamp array", func(t *testing.T) {
		in := batchOf(t, []int64{streamID}, []string{"one"})
		in.Timestamp = nil

		var batch readBatch
		require.ErrorContains(t, batch.fill(task, in), "without the stream ID or timestamp column")
	})
}

func TestReadBatch_Reset(t *testing.T) {
	const streamID = int64(7)
	task := lineTask(t, streamID)

	t.Run("it drops the records, including those past their length", func(t *testing.T) {
		batch := readBatch{records: make([]LogRecord, 0, 4)}
		batch.records = append(batch.records,
			LogRecord{streamLabels: labels.FromStrings("app", "a"), metadata: labels.FromStrings("level", "error")},
			LogRecord{streamLabels: labels.FromStrings("app", "b"), metadata: labels.FromStrings("level", "info")},
		)
		batch.records = batch.records[:1]

		batch.reset()
		require.Empty(t, batch.records)
		for _, record := range batch.records[:cap(batch.records)] {
			require.Equal(t, LogRecord{}, record)
		}
	})

	t.Run("it releases the line region that the last fill did not use", func(t *testing.T) {
		var batch readBatch
		require.NoError(t, batch.fill(task, batchOf(t, []int64{streamID}, []string{"short"})))
		batch.reset()

		long := strings.Repeat("x", 100)
		require.NoError(t, batch.fill(task, batchOf(t, []int64{streamID}, []string{long})))
		withBoth := batch.linesAlloc.AllocatedBytes()

		batch.reset()
		require.Less(t, batch.linesAlloc.AllocatedBytes(), withBoth)
		require.GreaterOrEqual(t, batch.linesAlloc.AllocatedBytes(), len(long))
	})

	t.Run("it keeps the line region after a fill that copied no line", func(t *testing.T) {
		var batch readBatch
		require.NoError(t, batch.fill(task, batchOf(t, []int64{streamID}, []string{"first"})))
		batch.reset()
		allocated := batch.linesAlloc.AllocatedBytes()

		require.NoError(t, batch.fill(task, batchOf(t, []int64{streamID}, []string{""})))
		batch.reset()
		require.Equal(t, allocated, batch.linesAlloc.AllocatedBytes())
	})
}

func TestReadBatchPool_Get(t *testing.T) {
	const batchSize = 4

	t.Run("on an empty pool it returns a new batch with no records", func(t *testing.T) {
		pool := newReadBatchPool(batchSize)

		batch := pool.get()
		require.Empty(t, batch.records)
		require.Equal(t, batchSize, cap(batch.records))
		require.False(t, batch.pooled)
	})

	t.Run("after a put a later get returns the put batch, though sync.Pool may drop some puts", func(t *testing.T) {
		pool := newReadBatchPool(batchSize)

		var reused bool
		for range 100 {
			batch := pool.get()
			pool.put(batch)
			if recycled := pool.get(); recycled == batch {
				require.False(t, recycled.pooled)
				reused = true
				break
			}
		}
		require.True(t, reused)
	})
}

func TestReadBatchPool_Put(t *testing.T) {
	const batchSize = 4

	t.Run("it resets the batch and marks it as pooled", func(t *testing.T) {
		pool := newReadBatchPool(batchSize)
		batch := pool.get()
		batch.records = append(batch.records, LogRecord{streamLabels: labels.FromStrings("app", "a")})

		pool.put(batch)
		require.Empty(t, batch.records)
		require.True(t, batch.pooled)
	})

	t.Run("it panics when the batch is already in the pool", func(t *testing.T) {
		pool := newReadBatchPool(batchSize)
		batch := pool.get()
		pool.put(batch)

		require.PanicsWithValue(t, "dataobjread: read batch returned to the pool twice", func() { pool.put(batch) })
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
				StructuredMetadata: push.LabelsAdapter{
					{Name: "pod", Value: fmt.Sprintf("pod-%d-%d", s, i/2500)},
					{Name: "trace_id", Value: fmt.Sprintf("%016x", uint64(i)*0x9e3779b97f4a7c15)},
					{Name: "detected_level", Value: levels[i%len(levels)]},
				},
			})
		}
		streams = append(streams, stream)
	}
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
					for reader.Next() {
						_ = reader.At()
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

// lineTask returns a task that forwards the lines of one admitted stream with the given ID.
func lineTask(t *testing.T, streamID int64) ReadTask {
	t.Helper()

	streamLabels, err := syntax.ParseLabels(`{app="a"}`)
	require.NoError(t, err)
	streams := newObjectStreams(
		map[int64]labels.Labels{streamID: streamLabels},
		func(labels.Labels, uint64) bool { return true },
	)
	return ReadTask{streamIDs: []int64{streamID}, streams: streams, needReturningLine: true}
}

// batchOf returns a batch with one row per line. Row i belongs to the stream streamIDs[i] and has
// the timestamp at(i+1).
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
