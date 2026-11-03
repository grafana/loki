package dataobjread

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/memory"
)

const (
	firstStreamID  = int64(7)
	secondStreamID = int64(8)

	// runsTaskPrefix starts every error that a task from newRunsTask returns.
	runsTaskPrefix = `data object "objects/ab/cd" logs section 3 `
)

func TestReadTask_ResolveRuns(t *testing.T) {
	t.Run("returns one run for each stretch of rows that share a stream", func(t *testing.T) {
		task := newRunsTask(t)

		runs, err := task.resolveRuns(newRunsBatch([]int64{firstStreamID, firstStreamID, secondStreamID, firstStreamID}), nil)
		require.NoError(t, err)
		require.Len(t, runs, 3)

		require.Equal(t, 2, runs[0].end)
		require.Equal(t, streamHashOf(`{app="a"}`), runs[0].stream.streamHash)
		require.Equal(t, 3, runs[1].end)
		require.Equal(t, streamHashOf(`{app="b"}`), runs[1].stream.streamHash)
		require.Equal(t, 4, runs[2].end)
		require.Equal(t, streamHashOf(`{app="a"}`), runs[2].stream.streamHash)
	})

	t.Run("returns a single run when every row has the same stream", func(t *testing.T) {
		task := newRunsTask(t)

		runs, err := task.resolveRuns(newRunsBatch([]int64{secondStreamID, secondStreamID, secondStreamID}), nil)
		require.NoError(t, err)
		require.Len(t, runs, 1)
		require.Equal(t, 3, runs[0].end)
	})

	t.Run("appends to the runs it is given", func(t *testing.T) {
		task := newRunsTask(t)
		existing := []streamRun{{end: 99}}

		runs, err := task.resolveRuns(newRunsBatch([]int64{firstStreamID}), existing)
		require.NoError(t, err)
		require.Len(t, runs, 2)
		require.Equal(t, 99, runs[0].end)
		require.Equal(t, 1, runs[1].end)
	})

	t.Run("fails on a row from a stream the task did not plan", func(t *testing.T) {
		task := newRunsTask(t)

		_, err := task.resolveRuns(newRunsBatch([]int64{firstStreamID, 9999}), nil)
		require.EqualError(t, err, runsTaskPrefix+"returned unexpected stream ID 9999")
	})

	t.Run("fails on a batch without the stream ID column", func(t *testing.T) {
		task := newRunsTask(t)
		batch := newRunsBatch([]int64{firstStreamID})
		batch.StreamID = nil

		_, err := task.resolveRuns(batch, nil)
		require.EqualError(t, err, runsTaskPrefix+"returned a batch without the stream ID or timestamp column")
	})

	t.Run("fails on a batch without the timestamp column", func(t *testing.T) {
		task := newRunsTask(t)
		batch := newRunsBatch([]int64{firstStreamID})
		batch.Timestamp = nil

		_, err := task.resolveRuns(batch, nil)
		require.EqualError(t, err, runsTaskPrefix+"returned a batch without the stream ID or timestamp column")
	})

	t.Run("names the row when a stream ID is null", func(t *testing.T) {
		task := newRunsTask(t)
		batch := newRunsBatch([]int64{firstStreamID, firstStreamID, firstStreamID})
		batch.StreamID = columnar.NewNumber([]int64{firstStreamID, firstStreamID, firstStreamID}, validity(true, false, true))

		_, err := task.resolveRuns(batch, nil)
		require.EqualError(t, err, runsTaskPrefix+"returned row 1 without a stream ID or a timestamp")
	})

	t.Run("names the row when a timestamp is null", func(t *testing.T) {
		task := newRunsTask(t)
		batch := newRunsBatch([]int64{firstStreamID, firstStreamID, firstStreamID})
		batch.Timestamp = columnar.NewNumber([]int64{1, 2, 3}, validity(true, true, false))

		_, err := task.resolveRuns(batch, nil)
		require.EqualError(t, err, runsTaskPrefix+"returned row 2 without a stream ID or a timestamp")
	})

	t.Run("fails when a column has fewer elements than the batch has rows", func(t *testing.T) {
		const mismatch = "returned a batch of 3 rows with a %s column of another length"
		streamIDs := []int64{firstStreamID, firstStreamID, firstStreamID}

		tests := map[string]func(batch *logs.Batch){
			"stream ID": func(batch *logs.Batch) { batch.StreamID = columnar.NewNumber(streamIDs[:2], memory.Bitmap{}) },
			"timestamp": func(batch *logs.Batch) { batch.Timestamp = columnar.NewNumber([]int64{1, 2}, memory.Bitmap{}) },
			"message":   func(batch *logs.Batch) { batch.Message = newUTF8Column("a", "b") },
			"metadata env": func(batch *logs.Batch) {
				batch.Metadata = []logs.MetadataColumn{{Name: "env", Values: newUTF8Column("prod")}}
			},
		}
		for column, shorten := range tests {
			batch := newRunsBatch(streamIDs, "a", "b", "c")
			shorten(batch)

			_, err := newRunsTask(t).resolveRuns(batch, nil)
			require.EqualError(t, err, runsTaskPrefix+fmt.Sprintf(mismatch, column))
		}
	})
}

func TestLogReader_At(t *testing.T) {
	t.Run("returns the stream, timestamp and line of the current row", func(t *testing.T) {
		reader := newAtReader(t, true, newRunsBatch([]int64{firstStreamID, secondStreamID}, "first", "second"), 1)

		record := reader.At()
		require.Equal(t, streamHashOf(`{app="b"}`), record.streamHash)
		require.Equal(t, `{app="b"}`, record.streamLabels.String())
		require.Equal(t, at(2).UnixNano(), record.timestamp)
		require.Equal(t, "second", string(record.line))
	})

	t.Run("caps the capacity of the line so an append cannot reach the next line", func(t *testing.T) {
		batch := newRunsBatch([]int64{firstStreamID, firstStreamID}, "first", "second")
		reader := newAtReader(t, true, batch, 0)

		record := reader.At()
		require.Equal(t, len(record.line), cap(record.line))

		_ = append(record.line, "XXXXXX"...)
		require.Equal(t, "second", string(batch.Message.Get(1)))
	})

	t.Run("returns no line when the task does not need it", func(t *testing.T) {
		reader := newAtReader(t, false, newRunsBatch([]int64{firstStreamID}, "line"), 0)

		require.Nil(t, reader.At().line)
	})

	t.Run("returns an empty line when the section has no message column", func(t *testing.T) {
		reader := newAtReader(t, true, newRunsBatch([]int64{firstStreamID}), 0)

		require.Empty(t, reader.At().line)
	})

	t.Run("returns no line for an empty value", func(t *testing.T) {
		reader := newAtReader(t, true, newRunsBatch([]int64{firstStreamID, firstStreamID}, "", "x"), 0)

		require.Nil(t, reader.At().line)
	})

	t.Run("builds the metadata labels of the current row and leaves out null values", func(t *testing.T) {
		batch := newRunsBatch([]int64{firstStreamID, firstStreamID})
		batch.Metadata = []logs.MetadataColumn{
			{Name: "env", Values: newUTF8ColumnWithNulls([]int{1}, "prod")},
			{Name: "trace_id", Values: newUTF8Column("t1", "t2")},
		}

		reader := newAtReader(t, false, batch, 0)
		require.Equal(t, labels.FromStrings("env", "prod", "trace_id", "t1"), reader.At().metadata)

		reader = newAtReader(t, false, batch, 1)
		require.Equal(t, labels.FromStrings("trace_id", "t2"), reader.At().metadata)
	})

	t.Run("gives each row only its own metadata keys when it reuses the builder", func(t *testing.T) {
		batch := newMetadataBatch()
		reader := newAtReader(t, false, batch, 0)

		for row, want := range metadataByRow() {
			reader.currRow = row
			reader.advanceRun()
			require.Equal(t, want, reader.At().metadata, "row %d", row)
		}
	})

	t.Run("keeps the metadata of earlier rows after later rows and after the batch memory changes", func(t *testing.T) {
		batch := newMetadataBatch()
		reader := newAtReader(t, false, batch, 0)

		var got []labels.Labels
		for row := range batch.Rows {
			reader.currRow = row
			reader.advanceRun()
			got = append(got, reader.At().metadata)
		}
		for _, column := range batch.Metadata {
			fill(column.Values.Data(), 'X')
		}

		require.Equal(t, metadataByRow(), got)
	})

	t.Run("returns the stored record of a row read", func(t *testing.T) {
		want := LogRecord{timestamp: 42, line: []byte("stored")}
		buf := &scanBuffer{}
		buf.setRecords([]LogRecord{want})
		reader := &LogReader{curr: buf}

		require.Equal(t, want, reader.At())
	})
}

func TestLogReader_Next(t *testing.T) {
	t.Run("yields every row of consecutive batches with the stream of its own run", func(t *testing.T) {
		reader, batches := newNextReader(t, 3)
		enqueueColumnar(t, reader, batches, []int64{firstStreamID, firstStreamID, secondStreamID, secondStreamID}, "a1", "a2", "b1", "b2")
		enqueueColumnar(t, reader, batches, []int64{secondStreamID}, "c1")
		enqueueRecords(t, reader, batches, LogRecord{timestamp: 100, line: []byte("r1")}, LogRecord{timestamp: 101, line: []byte("r2")})
		close(batches)

		var got []string
		for reader.Next() {
			record := reader.At()
			got = append(got, fmt.Sprintf("%s|%d|%s", record.streamLabels, record.timestamp, record.line))
		}

		require.Equal(t, []string{
			fmt.Sprintf(`{app="a"}|%d|a1`, at(1).UnixNano()),
			fmt.Sprintf(`{app="a"}|%d|a2`, at(2).UnixNano()),
			fmt.Sprintf(`{app="b"}|%d|b1`, at(3).UnixNano()),
			fmt.Sprintf(`{app="b"}|%d|b2`, at(4).UnixNano()),
			fmt.Sprintf(`{app="b"}|%d|c1`, at(1).UnixNano()),
			`{}|100|r1`,
			`{}|101|r2`,
		}, got)
	})

	t.Run("keeps a buffer until it moves past the batch and then clears it", func(t *testing.T) {
		reader, batches := newNextReader(t, 2)
		buf := enqueueColumnar(t, reader, batches, []int64{firstStreamID, firstStreamID}, "a1", "a2")
		close(batches)

		require.True(t, reader.Next())
		require.True(t, reader.Next())
		require.Len(t, reader.pool.free, 1, "the consumer still reads the last row of the batch")

		require.False(t, reader.Next())
		require.Len(t, reader.pool.free, 2)
		require.Nil(t, buf.batch)
	})

	t.Run("returns the buffer before it blocks for the next batch", func(t *testing.T) {
		reader, batches := newNextReader(t, 1)
		enqueueColumnar(t, reader, batches, []int64{firstStreamID}, "a1")
		require.True(t, reader.Next())

		next := make(chan bool, 1)
		go func() { next <- reader.Next() }()

		ctx, cancel := context.WithTimeout(t.Context(), readerTestTimeout)
		defer cancel()
		_, err := reader.pool.get(ctx)
		require.NoError(t, err, "Next must return its buffer before it waits for another batch")

		close(batches)
		require.False(t, <-next)
	})

	t.Run("keeps returning false when no batch ever arrives", func(t *testing.T) {
		reader, batches := newNextReader(t, 1)
		close(batches)

		require.False(t, reader.Next())
		require.False(t, reader.Next())
	})

	t.Run("keeps returning false after the last batch without returning its buffer twice", func(t *testing.T) {
		reader, batches := newNextReader(t, 2)
		enqueueColumnar(t, reader, batches, []int64{firstStreamID}, "a1")
		_, err := reader.pool.get(t.Context())
		require.NoError(t, err)
		close(batches)

		require.True(t, reader.Next())
		require.False(t, reader.Next())
		require.False(t, reader.Next())
		require.Len(t, reader.pool.free, 1)
	})

	t.Run("stops at the batch boundary and aborts the planner once a scan failed", func(t *testing.T) {
		reader, batches := newNextReader(t, 3)
		reader.tasks = queuedTasks()
		enqueueColumnar(t, reader, batches, []int64{firstStreamID}, "a1")
		enqueueColumnar(t, reader, batches, []int64{firstStreamID}, "a2")
		reader.setErr(errors.New("boom"))

		require.False(t, reader.Next())
		require.EqualError(t, reader.tasks.Err(), "boom")
	})
}

func newRunsTask(t *testing.T) ReadTask {
	t.Helper()

	first, err := syntax.ParseLabels(`{app="a"}`)
	require.NoError(t, err)
	second, err := syntax.ParseLabels(`{app="b"}`)
	require.NoError(t, err)

	streams := newObjectStreams(
		map[int64]labels.Labels{firstStreamID: first, secondStreamID: second},
		func(labels.Labels, uint64) bool { return true },
	)
	return ReadTask{objectPath: "objects/ab/cd", sectionIdx: 3, streamIDs: []int64{firstStreamID, secondStreamID}, streams: streams}
}

// newRunsBatch returns a batch with the given stream IDs and timestamps of one, two, three and so
// on seconds. It has a message column only when lines is not empty.
func newRunsBatch(streamIDs []int64, lines ...string) *logs.Batch {
	var timestamps []int64
	for i := range streamIDs {
		timestamps = append(timestamps, at(i+1).UnixNano())
	}

	batch := &logs.Batch{
		Rows:      len(streamIDs),
		StreamID:  columnar.NewNumber(streamIDs, memory.Bitmap{}),
		Timestamp: columnar.NewNumber(timestamps, memory.Bitmap{}),
	}
	if len(lines) > 0 {
		batch.Message = newUTF8Column(lines...)
	}
	return batch
}

// newAtReader returns a reader positioned on row of batch, as if Next had just returned true.
func newAtReader(t *testing.T, needLine bool, batch *logs.Batch, row int) *LogReader {
	t.Helper()

	runs, err := newRunsTask(t).resolveRuns(batch, nil)
	require.NoError(t, err)

	buf := &scanBuffer{}
	buf.setBatch(batch, runs, needLine)

	reader := &LogReader{curr: buf, currRow: row, metadataBuilder: labels.NewScratchBuilder(8)}
	reader.advanceRun()
	return reader
}

// newMetadataBatch returns a batch of four rows that have these metadata keys, in order: env and
// trace_id, none, trace_id, env. See metadataByRow.
func newMetadataBatch() *logs.Batch {
	batch := newRunsBatch([]int64{firstStreamID, firstStreamID, firstStreamID, firstStreamID})
	batch.Metadata = []logs.MetadataColumn{
		{Name: "env", Values: newUTF8ColumnWithNulls([]int{1, 2}, "prod", "dev")},
		{Name: "trace_id", Values: newUTF8ColumnWithNulls([]int{1, 3}, "t0", "t2")},
	}
	return batch
}

// metadataByRow returns the metadata labels of each row of newMetadataBatch.
func metadataByRow() []labels.Labels {
	return []labels.Labels{
		labels.FromStrings("env", "prod", "trace_id", "t0"),
		labels.EmptyLabels(),
		labels.FromStrings("trace_id", "t2"),
		labels.FromStrings("env", "dev"),
	}
}

// newNextReader returns a reader that has a pool of size buffers and no scans, and the channel it
// receives batches from. The test queues the batches itself.
func newNextReader(t *testing.T, size int) (*LogReader, chan *scanBuffer) {
	t.Helper()

	batches := make(chan *scanBuffer, size)
	return &LogReader{
		pool:            newBufferPool(size),
		batches:         batches,
		metadataBuilder: labels.NewScratchBuilder(8),
	}, batches
}

// enqueueColumnar takes a buffer from the pool of reader, fills it with a columnar read of the
// given rows, queues it and returns it.
func enqueueColumnar(t *testing.T, reader *LogReader, batches chan *scanBuffer, streamIDs []int64, lines ...string) *scanBuffer {
	t.Helper()

	batch := newRunsBatch(streamIDs, lines...)
	runs, err := newRunsTask(t).resolveRuns(batch, nil)
	require.NoError(t, err)

	buf, err := reader.pool.get(t.Context())
	require.NoError(t, err)
	buf.setBatch(batch, runs, true)
	batches <- buf
	return buf
}

// enqueueRecords takes a buffer from the pool of reader, fills it with a row read, queues it and
// returns it.
func enqueueRecords(t *testing.T, reader *LogReader, batches chan *scanBuffer, records ...LogRecord) *scanBuffer {
	t.Helper()

	buf, err := reader.pool.get(t.Context())
	require.NoError(t, err)
	buf.setRecords(records)
	batches <- buf
	return buf
}

func newUTF8Column(values ...string) *columnar.UTF8 {
	builder := columnar.NewUTF8Builder(memory.NewAllocator(nil))
	for _, value := range values {
		builder.AppendValue([]byte(value))
	}
	return builder.Build()
}

// newUTF8ColumnWithNulls returns a column that has a null element at each index of nulls and the
// given values at the other indexes, in order.
func newUTF8ColumnWithNulls(nulls []int, values ...string) *columnar.UTF8 {
	builder := columnar.NewUTF8Builder(memory.NewAllocator(nil))
	next := 0
	for i := range len(values) + len(nulls) {
		if slices.Contains(nulls, i) {
			builder.AppendNull()
			continue
		}
		builder.AppendValue([]byte(values[next]))
		next++
	}
	return builder.Build()
}

func validity(values ...bool) memory.Bitmap {
	bitmap := memory.NewBitmap(nil, len(values))
	bitmap.AppendValues(values...)
	return bitmap
}
