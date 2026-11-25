package dataobjread

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestRowsBatch_Init(t *testing.T) {
	// run is a streamRun reduced to the app label of its stream and its bounds.
	type run struct {
		app        string
		start, end int
	}
	runsOf := func(runs []streamRun) []run {
		out := make([]run, 0, len(runs))
		for _, r := range runs {
			out = append(out, run{app: r.stream.streamLabels.Get("app"), start: r.start, end: r.end})
		}
		return out
	}

	pool := newRowsBatchPool()
	task := twoStreamTask()

	t.Run("it holds the records as they are", func(t *testing.T) {
		records := batchOf(t, []int64{7, 7}, []string{"one", "two"})

		batch := pool.get()
		require.NoError(t, batch.init(task, records))

		require.Same(t, records, batch.records)
	})

	t.Run("records of one stream form one run with the identity of the stream", func(t *testing.T) {
		batch := pool.get()
		require.NoError(t, batch.init(task, batchOf(t, []int64{7, 7, 7}, []string{"one", "two", "three"})))

		require.Equal(t, []streamRun{{stream: task.streams.admittedByID[7], start: 0, end: 3}}, batch.streamRuns)
	})

	t.Run("each change of stream starts a run, and a stream that returns starts another", func(t *testing.T) {
		batch := pool.get()
		require.NoError(t, batch.init(task, batchOf(t, []int64{7, 7, 8, 8, 8, 7}, []string{"a", "b", "c", "d", "e", "f"})))

		require.Equal(t, []run{{"a", 0, 2}, {"b", 2, 5}, {"a", 5, 6}}, runsOf(batch.streamRuns))
	})

	t.Run("the runs cover every record once, in record order", func(t *testing.T) {
		ids := []int64{7, 7, 8, 7, 7, 7, 8, 8, 7}
		batch := pool.get()
		require.NoError(t, batch.init(task, batchOf(t, ids, make([]string, len(ids)))))

		next := 0
		for _, r := range batch.streamRuns {
			require.Equal(t, next, r.start)
			require.Greater(t, r.end, r.start)
			next = r.end
		}
		require.Equal(t, len(ids), next)
	})

	t.Run("the batch has the line when the task needs it and the records have a message array", func(t *testing.T) {
		batch := pool.get()
		require.NoError(t, batch.init(task, batchOf(t, []int64{7}, []string{"one"})))

		require.True(t, batch.hasLine)
	})

	t.Run("the batch has no line when the task does not need it", func(t *testing.T) {
		noLine := task
		noLine.needReturningLine = false

		batch := pool.get()
		require.NoError(t, batch.init(noLine, batchOf(t, []int64{7}, []string{"one"})))

		require.False(t, batch.hasLine)
	})

	t.Run("the batch has no line when the records have no message array", func(t *testing.T) {
		records := batchOf(t, []int64{7}, []string{"one"})
		records.Message = nil

		batch := pool.get()
		require.NoError(t, batch.init(task, records))

		require.False(t, batch.hasLine)
	})

	t.Run("the batch has metadata when the records have a metadata column", func(t *testing.T) {
		batch := pool.get()
		require.NoError(t, batch.init(task, withLevelMetadata(batchOf(t, []int64{7}, []string{"one"}))))

		require.True(t, batch.hasMetadata)
	})

	t.Run("the batch has no metadata when the records have no metadata column", func(t *testing.T) {
		batch := pool.get()
		require.NoError(t, batch.init(task, batchOf(t, []int64{7}, []string{"one"})))

		require.False(t, batch.hasMetadata)
	})

	t.Run("a record from a stream the task did not plan fails, and the batch holds no records", func(t *testing.T) {
		batch := pool.get()

		err := batch.init(task, batchOf(t, []int64{7, 9999}, []string{"one", "two"}))

		require.EqualError(t, err, `data object "objects/ab/cd" logs section 3 returned unexpected stream ID 9999`)
		require.Nil(t, batch.records)
	})

	t.Run("a record from a stream that a filter dropped fails too", func(t *testing.T) {
		streams := newObjectStreams(
			map[int64]labels.Labels{7: labels.FromStrings("app", "a")},
			func(labels.Labels, uint64) bool { return false }, // decoded, not admitted
		)
		dropped := ReadTask{objectPath: "objects/ab/cd", sectionIdx: 3, streams: streams}

		err := pool.get().init(dropped, batchOf(t, []int64{7}, []string{"one"}))

		require.ErrorContains(t, err, "unexpected stream ID 7")
	})

	t.Run("records without a stream ID array fail, and the batch holds no records", func(t *testing.T) {
		records := batchOf(t, []int64{7}, []string{"one"})
		records.StreamID = nil
		batch := pool.get()

		err := batch.init(task, records)

		require.ErrorContains(t, err, "without the stream ID or timestamp column")
		require.Nil(t, batch.records)
	})

	t.Run("records without a timestamp array fail, and the batch holds no records", func(t *testing.T) {
		records := batchOf(t, []int64{7}, []string{"one"})
		records.Timestamp = nil
		batch := pool.get()

		err := batch.init(task, records)

		require.ErrorContains(t, err, "without the stream ID or timestamp column")
		require.Nil(t, batch.records)
	})
}

func TestRowsBatch_Line(t *testing.T) {
	pool := newRowsBatchPool()

	t.Run("returns the line of the record when the batch has the line", func(t *testing.T) {
		batch := newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"},
		)

		require.Equal(t, "one", string(batch.line(0)))
		require.Equal(t, "two", string(batch.line(1)))
	})

	t.Run("returns no line when the batch has no line", func(t *testing.T) {
		batch := newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"})
		batch.hasLine = false

		require.Nil(t, batch.line(0))
	})

	t.Run("returns no line for an empty message and for a null message", func(t *testing.T) {
		batch := newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "x"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "x"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 3, line: "x"},
		)
		messages := columnar.NewUTF8Builder(&batch.alloc)
		messages.AppendValue(nil)
		messages.AppendNull()
		messages.AppendValue([]byte("three"))
		batch.records.Message = messages.Build()

		require.Nil(t, batch.line(0))
		require.Nil(t, batch.line(1))
		require.Equal(t, "three", string(batch.line(2)))
	})

	t.Run("caps each line at its own length, so an append does not overwrite the next line", func(t *testing.T) {
		batch := newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"},
		)

		first := batch.line(0)
		require.Equal(t, len(first), cap(first))
		_ = append(first, "xxxx"...)
		require.Equal(t, "two", string(batch.line(1)))
	})
}

func TestRowsBatch_Release(t *testing.T) {
	task := twoStreamTask()

	t.Run("it drops the records and the runs and marks the batch as pooled", func(t *testing.T) {
		batch := newRowsBatchPool().get()
		require.NoError(t, batch.init(task, withLevelMetadata(batchOf(t, []int64{7}, []string{"one"}))))

		batch.release()
		require.True(t, batch.pooled)
		require.Nil(t, batch.records)
		require.Empty(t, batch.streamRuns)
		require.False(t, batch.hasLine)
		require.False(t, batch.hasMetadata)
	})

	t.Run("it clears the runs that a failed init left, because they hold the labels of a stream", func(t *testing.T) {
		batch := newRowsBatchPool().get()
		require.Error(t, batch.init(task, batchOf(t, []int64{7, 8, 9999}, []string{"one", "two", "three"})))

		batch.release()
		require.Empty(t, batch.streamRuns)
		for _, run := range batch.streamRuns[:cap(batch.streamRuns)] {
			require.Equal(t, streamRun{}, run)
		}
	})

	t.Run("it reclaims the memory of the batch and keeps the regions for the next read", func(t *testing.T) {
		batch := newRowsBatchPool().get()
		batch.alloc.Allocate(1024)
		require.Zero(t, batch.alloc.FreeBytes())
		allocated := batch.alloc.AllocatedBytes()

		batch.release()
		require.Equal(t, allocated, batch.alloc.AllocatedBytes())
		require.Equal(t, allocated, batch.alloc.FreeBytes())
	})

	t.Run("reset keeps the regions of an earlier larger read, because it never trims", func(t *testing.T) {
		batch := newRowsBatchPool().get()
		batch.alloc.Allocate(1024)
		batch.alloc.Allocate(4096)
		batch.reset()
		allocated := batch.alloc.AllocatedBytes()

		batch.alloc.Allocate(8)
		batch.reset()
		require.Equal(t, allocated, batch.alloc.AllocatedBytes())
	})

	t.Run("it panics when the batch is still in the pool", func(t *testing.T) {
		batch := newRowsBatchPool().get()
		batch.release()

		require.PanicsWithValue(t, "dataobjread: batch released twice", func() { batch.release() })
	})

	t.Run("a second release panics before it reclaims any memory", func(t *testing.T) {
		batch := newRowsBatchPool().get()
		batch.release()
		batch.alloc.Allocate(8)

		require.PanicsWithValue(t, "dataobjread: batch released twice", func() { batch.release() })
		require.Zero(t, batch.alloc.FreeBytes())
	})

	t.Run("it overwrites the memory of the batch in a test", func(t *testing.T) {
		skipWithoutPoisoning(t)
		batch := newRowsBatchPool().get()
		region := batch.alloc.Allocate(64).Data()
		copy(region, "a line that a consumer must not read after the release")

		batch.release()
		for i, b := range region {
			require.EqualValues(t, 0xA5, b, "byte %d", i)
		}
		require.Equal(t, batch.alloc.AllocatedBytes(), batch.alloc.FreeBytes(), "the poisoned regions are free again")
	})
}

// skipWithoutPoisoning skips the test in a run that includes benchmarks, where a pool does not
// overwrite the memory of a released batch.
func skipWithoutPoisoning(t *testing.T) {
	t.Helper()

	if !poisonReleasedBatches() {
		t.Skip("a run that includes benchmarks does not poison released batches")
	}
}

// withLevelMetadata adds a level metadata column to records, and returns them.
func withLevelMetadata(records *logs.RecordBatch) *logs.RecordBatch {
	values := columnar.NewUTF8Builder(memory.NewAllocator(nil))
	for range records.Rows {
		values.AppendValue([]byte("error"))
	}
	records.Metadata = []logs.MetadataColumn{{Name: "level", Values: values.Build()}}
	return records
}

// twoStreamTask returns a task that admits the streams 7 and 8, and needs the line.
func twoStreamTask() ReadTask {
	streams := newObjectStreams(
		map[int64]labels.Labels{7: labels.FromStrings("app", "a"), 8: labels.FromStrings("app", "b")},
		func(labels.Labels, uint64) bool { return true },
	)
	return ReadTask{objectPath: "objects/ab/cd", sectionIdx: 3, streamIDs: []int64{7, 8}, streams: streams, needReturningLine: true}
}
