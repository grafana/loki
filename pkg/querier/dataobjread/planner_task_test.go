package dataobjread

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestReadTask_RowPredicates(t *testing.T) {
	t.Run("the time window comes first and is start-inclusive, end-exclusive", func(t *testing.T) {
		task := ReadTask{start: at(1), end: at(5)}

		predicates := task.rowPredicates()
		require.Len(t, predicates, 1)
		require.Equal(t, logs.TimeRangeRowPredicate{
			StartTime:    at(1),
			EndTime:      at(5),
			IncludeStart: true,
			IncludeEnd:   false,
		}, predicates[0])
	})

	t.Run("the planned metadata predicates follow the time window", func(t *testing.T) {
		task := ReadTask{
			start: at(1),
			end:   at(5),
			predicates: []logs.RowPredicate{
				logs.MetadataMatcherRowPredicate{Key: "level", Value: "error"},
			},
		}

		predicates := task.rowPredicates()
		require.Len(t, predicates, 2)
		require.IsType(t, logs.TimeRangeRowPredicate{}, predicates[0])
		require.Equal(t, logs.MetadataMatcherRowPredicate{Key: "level", Value: "error"}, predicates[1])
	})
}

func TestReadTask_Records(t *testing.T) {
	// oneStreamTask returns a task for a single admitted stream, and that stream's ID.
	oneStreamTask := func(t *testing.T, streamLabels string) (ReadTask, int64) {
		t.Helper()
		parsed, err := syntax.ParseLabels(streamLabels)
		require.NoError(t, err)

		const id = int64(7)
		streams := newObjectStreams(
			map[int64]labels.Labels{id: parsed},
			func(labels.Labels, uint64) bool { return true },
		)
		return ReadTask{objectPath: "objects/ab/cd", sectionIdx: 3, streamIDs: []int64{id}, streams: streams}, id
	}

	t.Run("it attaches each row's stream identity", func(t *testing.T) {
		task, id := oneStreamTask(t, `{app="a"}`)

		records, err := task.records([]logs.Record{
			{StreamID: id, Timestamp: at(1), Line: []byte("one")},
			{StreamID: id, Timestamp: at(2), Line: []byte("two")},
		})
		require.NoError(t, err)

		streamLabels, err := syntax.ParseLabels(`{app="a"}`)
		require.NoError(t, err)
		require.Equal(t, []LogRecord{
			{
				streamHash:   streamHashOf(`{app="a"}`),
				streamLabels: streamLabels,
				timestamp:    at(1).UnixNano(),
				line:         []byte("one"),
			},
			{
				streamHash:   streamHashOf(`{app="a"}`),
				streamLabels: streamLabels,
				timestamp:    at(2).UnixNano(),
				line:         []byte("two"),
			},
		}, records)
	})

	t.Run("it copies the line, because the row reader reuses that buffer", func(t *testing.T) {
		task, id := oneStreamTask(t, `{app="a"}`)

		// One buffer, rewritten between reads, is what the row reader does. The replacement is
		// the same length as the original, so it overwrites every byte.
		buffer := []byte("first")
		first, err := task.records([]logs.Record{{StreamID: id, Timestamp: at(1), Line: buffer}})
		require.NoError(t, err)

		copy(buffer, []byte("third"))
		require.Equal(t, "first", string(first[0].line), "the record must not alias the read buffer")
	})

	t.Run("a row from a stream the task did not plan fails rather than get silently skipped", func(t *testing.T) {
		task, _ := oneStreamTask(t, `{app="a"}`)

		_, err := task.records([]logs.Record{{StreamID: 9999, Timestamp: at(1), Line: []byte("one")}})
		require.ErrorContains(t, err, "unexpected stream ID 9999")
	})

	t.Run("a row from a stream a filter dropped fails too", func(t *testing.T) {
		parsed, err := syntax.ParseLabels(`{app="a"}`)
		require.NoError(t, err)

		const id = int64(7)
		streams := newObjectStreams(
			map[int64]labels.Labels{id: parsed},
			func(labels.Labels, uint64) bool { return false }, // decoded, not admitted
		)
		task := ReadTask{objectPath: "objects/ab/cd", sectionIdx: 3, streams: streams}

		_, err = task.records([]logs.Record{{StreamID: id, Timestamp: at(1), Line: []byte("one")}})
		require.ErrorContains(t, err, "unexpected stream ID 7")
	})

	t.Run("no rows yields no records", func(t *testing.T) {
		task, _ := oneStreamTask(t, `{app="a"}`)

		records, err := task.records(nil)
		require.NoError(t, err)
		require.Empty(t, records)
	})
}

func TestObjectStreams(t *testing.T) {
	parse := func(t *testing.T, s string) labels.Labels {
		t.Helper()
		parsed, err := syntax.ParseLabels(s)
		require.NoError(t, err)
		return parsed
	}

	t.Run("an admitted stream is both decoded and admitted", func(t *testing.T) {
		streams := newObjectStreams(
			map[int64]labels.Labels{1: parse(t, `{app="a"}`)},
			func(labels.Labels, uint64) bool { return true },
		)
		require.True(t, streams.decoded(1))
		require.True(t, streams.admits(1))
	})

	t.Run("a stream a filter dropped is decoded but not admitted", func(t *testing.T) {
		streams := newObjectStreams(
			map[int64]labels.Labels{1: parse(t, `{app="a"}`)},
			func(labels.Labels, uint64) bool { return false },
		)
		require.True(t, streams.decoded(1), "the read did return it")
		require.False(t, streams.admits(1))
	})

	t.Run("a stream a filter dropped keeps only its ID, not its labels", func(t *testing.T) {
		streams := newObjectStreams(
			map[int64]labels.Labels{
				1: parse(t, `{app="kept"}`),
				2: parse(t, `{app="dropped"}`),
			},
			func(streamLabels labels.Labels, _ uint64) bool {
				return streamLabels.Get("app") == "kept"
			},
		)

		// A query whose policy denies most of its streams would otherwise hold a label set for
		// every denied one until it finished.
		require.Contains(t, streams.admittedByID, int64(1))
		require.NotContains(t, streams.admittedByID, int64(2))
		require.Contains(t, streams.notAdmittedByID, int64(2))
	})

	t.Run("the admitted and dropped sets never hold the same stream", func(t *testing.T) {
		streams := newObjectStreams(
			map[int64]labels.Labels{
				1: parse(t, `{app="a"}`),
				2: parse(t, `{app="b"}`),
				3: parse(t, `{app="c"}`),
			},
			func(streamLabels labels.Labels, _ uint64) bool {
				return streamLabels.Get("app") != "b"
			},
		)

		for id := range streams.admittedByID {
			require.NotContains(t, streams.notAdmittedByID, id)
		}
		require.Len(t, streams.admittedByID, 2)
		require.Len(t, streams.notAdmittedByID, 1)
	})

	t.Run("a stream the read never returned is neither decoded nor admitted", func(t *testing.T) {
		streams := newObjectStreams(nil, func(labels.Labels, uint64) bool { return true })
		require.False(t, streams.decoded(1))
		require.False(t, streams.admits(1))
	})

	t.Run("each stream carries the stream hash of its own labels", func(t *testing.T) {
		streams := newObjectStreams(
			map[int64]labels.Labels{
				1: parse(t, `{app="a"}`),
				2: parse(t, `{app="b"}`),
			},
			func(labels.Labels, uint64) bool { return true },
		)
		require.Equal(t, streamHashOf(`{app="a"}`), streams.admittedByID[1].streamHash)
		require.Equal(t, streamHashOf(`{app="b"}`), streams.admittedByID[2].streamHash)
	})
}

func TestTaskIterator(t *testing.T) {
	t.Run("it yields every queued task", func(t *testing.T) {
		it := queuedTasks(ReadTask{sectionIdx: 1}, ReadTask{sectionIdx: 2})

		var got []int
		for it.Next() {
			got = append(got, it.At().sectionIdx)
		}
		require.Equal(t, []int{1, 2}, got)
		require.NoError(t, it.Err())
	})

	t.Run("a recorded error stops iteration before the tasks queued ahead of it", func(t *testing.T) {
		it := queuedTasks(ReadTask{sectionIdx: 1}, ReadTask{sectionIdx: 2})
		it.setErr(errors.New("resolution failed"))

		require.False(t, it.Next())
		require.ErrorContains(t, it.Err(), "resolution failed")
	})

	t.Run("the first recorded error is the one reported", func(t *testing.T) {
		it := queuedTasks()
		it.setErr(errors.New("first"))
		it.setErr(errors.New("second"))
		require.ErrorContains(t, it.Err(), "first")
	})

	t.Run("Abort records its error, cancels the planner and waits for it", func(t *testing.T) {
		ch := make(chan ReadTask)
		cancelled := make(chan struct{})
		it := newTaskIterator(ch, func() { close(cancelled) })

		// A planner that exits when cancelled, as the real one does.
		go func() {
			<-cancelled
			close(ch)
			close(it.done)
		}()

		it.Abort(errors.New("reader failed"))
		require.ErrorContains(t, it.Err(), "reader failed")
	})

	t.Run("Abort reports the error it is given, even a cancellation", func(t *testing.T) {
		it := queuedTasks()
		it.Abort(context.Canceled)
		require.ErrorIs(t, it.Err(), context.Canceled, "an error Abort is given is the caller reporting a failure")
	})

	t.Run("Abort with no error drops the cancellation it causes", func(t *testing.T) {
		ch := make(chan ReadTask)
		cancelled := make(chan struct{})
		it := newTaskIterator(ch, func() { close(cancelled) })

		go func() {
			<-cancelled
			// What the real planner does when its context is cancelled.
			it.setErr(context.Canceled)
			close(ch)
			close(it.done)
		}()

		it.Abort(nil)
		require.NoError(t, it.Err(), "stopping early is not a query failure")
	})

	t.Run("Abort is safe to call again after a normal drain", func(t *testing.T) {
		it := queuedTasks(ReadTask{sectionIdx: 1})
		for it.Next() {
		}
		it.Abort(nil)
		it.Abort(nil)
		require.NoError(t, it.Err())
	})

	t.Run("Waited measures the time spent waiting for a planner that runs behind", func(t *testing.T) {
		const lag = 50 * time.Millisecond

		ch := make(chan ReadTask)
		it := newTaskIterator(ch, func() {})
		close(it.done)

		go func() {
			time.Sleep(lag)
			ch <- ReadTask{sectionIdx: 1}
			close(ch)
		}()

		for it.Next() {
		}
		require.GreaterOrEqual(t, it.Waited(), lag)
	})

	t.Run("it panics on a nil channel, which would otherwise deadlock", func(t *testing.T) {
		require.Panics(t, func() { newTaskIterator(nil, func() {}) })
	})
}

// queuedTasks returns an iterator over the given tasks whose planner has already finished.
func queuedTasks(tasks ...ReadTask) *TaskIterator {
	ch := make(chan ReadTask, len(tasks))
	for _, task := range tasks {
		ch <- task
	}
	close(ch)
	it := newTaskIterator(ch, func() {})
	close(it.done)
	return it
}
