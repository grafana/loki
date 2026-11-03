package dataobjread

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestReadTask_RecordsFromBatch(t *testing.T) {
	const id = int64(7)

	newTask := func(t *testing.T) ReadTask {
		t.Helper()

		parsed, err := syntax.ParseLabels(`{app="a"}`)
		require.NoError(t, err)

		streams := newObjectStreams(
			map[int64]labels.Labels{id: parsed},
			func(labels.Labels, uint64) bool { return true },
		)
		return ReadTask{objectPath: "objects/ab/cd", sectionIdx: 3, streamIDs: []int64{id}, streams: streams, needReturningLine: true}
	}

	newBatch := func(streamIDs []int64, lines ...string) *logs.Batch {
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
			builder := columnar.NewUTF8Builder(memory.NewAllocator(nil))
			for _, line := range lines {
				builder.AppendValue([]byte(line))
			}
			batch.Message = builder.Build()
		}
		return batch
	}

	t.Run("attaches each row's stream identity and copies its line", func(t *testing.T) {
		task := newTask(t)
		batch := newBatch([]int64{id, id}, "first", "second")

		records, err := task.recordsFromBatch(batch)
		require.NoError(t, err)
		require.Len(t, records, 2)
		require.Equal(t, streamHashOf(`{app="a"}`), records[0].streamHash)
		require.Equal(t, at(1).UnixNano(), records[0].timestamp)
		require.Equal(t, "first", string(records[0].line))
		require.Equal(t, "second", string(records[1].line))

		copy(batch.Message.Data(), "XXXXX")
		require.Equal(t, "first", string(records[0].line), "the record must not alias the batch memory")
	})

	t.Run("forwards no line when the task does not need it", func(t *testing.T) {
		task := newTask(t)
		task.needReturningLine = false

		records, err := task.recordsFromBatch(newBatch([]int64{id}, "line"))
		require.NoError(t, err)
		require.Nil(t, records[0].line)
	})

	t.Run("reads an empty line when the section has no message column", func(t *testing.T) {
		task := newTask(t)

		records, err := task.recordsFromBatch(newBatch([]int64{id}))
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Empty(t, records[0].line)
	})

	t.Run("fails on a row from a stream the task did not plan", func(t *testing.T) {
		task := newTask(t)

		_, err := task.recordsFromBatch(newBatch([]int64{9999}, "line"))
		require.ErrorContains(t, err, "unexpected stream ID 9999")
	})

	t.Run("fails on a batch without the stream ID column", func(t *testing.T) {
		task := newTask(t)
		batch := newBatch([]int64{id}, "line")
		batch.StreamID = nil

		_, err := task.recordsFromBatch(batch)
		require.ErrorContains(t, err, "without the stream ID or timestamp column")
	})
}
