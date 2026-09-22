package dataobjread

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/pkg/push"
)

func TestLogReader(t *testing.T) {
	metrics := NewMetrics(nil)

	t.Run("it yields every record of every planned section", func(t *testing.T) {
		fixture := newReaderFixture(t,
			logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(1, "one"), entry(2, "two")}},
			logproto.Stream{Labels: `{app="b"}`, Entries: []push.Entry{entry(1, "three")}},
		)
		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)

		lines := drainReader(reader)
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
		require.ElementsMatch(t, []string{"one", "two", "three"}, lines)
	})

	t.Run("a batch size below the row count still yields every distinct line", func(t *testing.T) {
		// Each record's line comes out of a buffer the row reader reuses between reads, so a
		// section read in several batches is what proves the reader copies it. With the copy
		// removed, the earlier lines of a batch come back as the last line read.
		var entries []push.Entry
		want := make([]string, 0, 10)
		for i := range 10 {
			line := fmt.Sprintf("line-%d", i)
			entries = append(entries, entry(i+1, line))
			want = append(want, line)
		}
		fixture := newReaderFixture(t, logproto.Stream{Labels: `{app="a"}`, Entries: entries})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), 1, 1, metrics)
		lines := drainReader(reader)
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
		require.ElementsMatch(t, want, lines)
	})

	t.Run("a task naming a section the object does not hold fails the read", func(t *testing.T) {
		fixture := newReaderFixture(t, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(1, "one")}})

		task := fixture.tasks[0]
		task.sectionIdx = 999

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(task), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))
		require.ErrorContains(t, reader.Err(), "holds no logs section 999")
		require.Error(t, reader.Close())
	})

	t.Run("a task naming an object the bucket does not hold fails the read", func(t *testing.T) {
		fixture := newReaderFixture(t, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(1, "one")}})

		task := fixture.tasks[0]
		task.objectPath = "objects/does-not-exist"

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(task), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))
		require.Error(t, reader.Err())
		require.Error(t, reader.Close())
	})

	t.Run("one failing scan does not stop the reader reporting the failure", func(t *testing.T) {
		fixture := newReaderFixture(t,
			logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(1, "one")}},
			logproto.Stream{Labels: `{app="b"}`, Entries: []push.Entry{entry(1, "two")}},
		)

		broken := fixture.tasks[0]
		broken.sectionIdx = 999
		tasks := append([]readTask{broken}, fixture.tasks...)

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		drainReader(reader)
		require.Error(t, reader.Err(), "a failed scan must not be reported as success")
		require.Error(t, reader.Close())
	})

	t.Run("a cancelled context is reported rather than looking like a clean end", func(t *testing.T) {
		fixture := newReaderFixture(t, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(1, "one")}})

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		reader := NewLogReader(ctx, fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		drainReader(reader)
		require.ErrorIs(t, reader.Err(), context.Canceled, "a cancelled query must not return a truncated result with no error")
	})

	t.Run("a planning error surfaces even when no task was planned", func(t *testing.T) {
		fixture := newReaderFixture(t, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(1, "one")}})

		tasks := queuedTasks()
		wantErr := errors.New("resolution failed")
		tasks.setErr(wantErr)

		reader := NewLogReader(t.Context(), fixture.objects, tasks, DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))
		require.ErrorIs(t, reader.Err(), wantErr)
	})

	t.Run("closing before the records are drained reports no error", func(t *testing.T) {
		fixture := newReaderFixture(t, logproto.Stream{
			Labels:  `{app="a"}`,
			Entries: []push.Entry{entry(1, "one"), entry(2, "two"), entry(3, "three")},
		})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.True(t, reader.Next())
		require.NoError(t, reader.Close())
	})

	t.Run("a cancellation the caller caused is still reported after Close", func(t *testing.T) {
		// Close suppresses the cancellation it causes itself. It must not suppress one the
		// caller caused, even when no scan has reported it yet: the query did fail, and a
		// truncated result would otherwise look authoritative.
		fixture := newReaderFixture(t, logproto.Stream{
			Labels:  `{app="a"}`,
			Entries: []push.Entry{entry(1, "one"), entry(2, "two")},
		})

		ctx, cancel := context.WithCancel(t.Context())
		reader := NewLogReader(ctx, fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		cancel()

		require.ErrorIs(t, reader.Close(), context.Canceled)
		require.ErrorIs(t, reader.Err(), context.Canceled)
	})

	t.Run("closing twice reports no error the second time", func(t *testing.T) {
		fixture := newReaderFixture(t, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(1, "one")}})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		drainReader(reader)
		require.NoError(t, reader.Close())
		require.NoError(t, reader.Close())
	})

	t.Run("a plan with no task yields nothing and reports no error", func(t *testing.T) {
		fixture := newReaderFixture(t, logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(1, "one")}})

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
		require.Empty(t, drainReader(reader))
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
	})
}

// TestLogReader_ConcurrentObjectOpens drives the open-object registry the way the planner
// and the scans do: several goroutines asking for one path at once. Only one opened object may
// win, because the sections it holds are opened lazily behind its own lock.
func TestLogReader_ConcurrentObjectOpens(t *testing.T) {
	fixture := newObjectsFixture(t, "", logproto.Stream{
		Labels:  `{app="a"}`,
		Entries: []push.Entry{entry(1, "one")},
	})
	objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
	t.Cleanup(objects.release)

	path := fixture.descriptors[0].ObjectPath
	const goroutines = 32

	results := make(chan *openObject, goroutines)
	errs := make(chan error, goroutines)
	start := make(chan struct{})
	for range goroutines {
		go func() {
			<-start
			object, err := objects.get(t.Context(), path)
			if err != nil {
				errs <- err
				return
			}
			results <- object
		}()
	}
	close(start)

	var first *openObject
	for range goroutines {
		select {
		case err := <-errs:
			require.NoError(t, err)
		case object := <-results:
			if first == nil {
				first = object
				continue
			}
			require.Same(t, first, object, "every caller must share one opened object")
		}
	}
}

// readerFixture is an open-object registry together with the real tasks a plan produced over it,
// so a reader test drives the same tasks production would.
type readerFixture struct {
	objects *OpenObjects
	tasks   []readTask
}

func newReaderFixture(t *testing.T, streams ...logproto.Stream) readerFixture {
	t.Helper()

	fixture := newObjectsFixture(t, "", streams...)
	objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
	t.Cleanup(objects.release)

	// bytes_over_time projects the message column, which the reader tests need: the line is the
	// one field the row reader hands back out of a reused buffer.
	expr, err := syntax.ParseSampleExpr(`bytes_over_time({app=~".+"}[1m])`)
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
