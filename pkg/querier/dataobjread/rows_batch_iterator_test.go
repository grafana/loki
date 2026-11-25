package dataobjread

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestRowsBatchIterator(t *testing.T) {
	pool := newRowsBatchPool()

	t.Run("it yields each row in record order with its timestamp, line, metadata and stream", func(t *testing.T) {
		it := newTestRowsBatchIterator(t, newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one", metadata: `{level="error"}`},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"},
			testRow{streamLabels: `{app="b"}`, timestampSec: 3, line: "three", metadata: `{level="info"}`},
		))

		require.Equal(t, []rowView{
			{TimestampSec: 1, Line: "one", Metadata: `{level="error"}`, Stream: `{app="a"}`, StreamHash: streamHashOf(`{app="a"}`), StartsRun: true},
			{TimestampSec: 2, Line: "two", Metadata: `{}`, Stream: `{app="a"}`, StreamHash: streamHashOf(`{app="a"}`)},
			{TimestampSec: 3, Line: "three", Metadata: `{level="info"}`, Stream: `{app="b"}`, StreamHash: streamHashOf(`{app="b"}`), StartsRun: true},
		}, drainRows(it))
	})

	t.Run("it starts a run at every change of stream, including a return to an earlier stream", func(t *testing.T) {
		it := newTestRowsBatchIterator(t, newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"},
			testRow{streamLabels: `{app="b"}`, timestampSec: 3, line: "three"},
			testRow{streamLabels: `{app="b"}`, timestampSec: 4, line: "four"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 5, line: "five"},
		))

		var startsRun []bool
		for _, got := range drainRows(it) {
			startsRun = append(startsRun, got.StartsRun)
		}
		require.Equal(t, []bool{true, false, true, false, true}, startsRun)
	})

	t.Run("it yields no row for a batch without runs", func(t *testing.T) {
		it := newTestRowsBatchIterator(t, pool.get())

		require.False(t, it.Next())
		require.Zero(t, *it.At())
	})

	t.Run("it returns the zero row before the first Next", func(t *testing.T) {
		it := newTestRowsBatchIterator(t, newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"}))

		require.Zero(t, *it.At())
	})

	t.Run("it keeps returning false and the zero row after the last row", func(t *testing.T) {
		it := newTestRowsBatchIterator(t, newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"}))

		require.True(t, it.Next())
		require.False(t, it.Next())
		require.False(t, it.Next())
		require.Zero(t, *it.At())
	})

	t.Run("it returns no line when the batch has no line", func(t *testing.T) {
		batch := newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"})
		batch.hasLine = false
		it := newTestRowsBatchIterator(t, batch)

		require.True(t, it.Next())
		require.Nil(t, it.At().line)
	})

	t.Run("it returns empty metadata when the batch has no metadata column", func(t *testing.T) {
		it := newTestRowsBatchIterator(t, newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"}))

		require.True(t, it.Next())
		require.Equal(t, labels.EmptyLabels(), it.At().metadata)
	})

	t.Run("the zero iterator yields no row and closes without effect", func(t *testing.T) {
		var it rowsBatchIterator

		require.False(t, it.Next())
		require.Zero(t, *it.At())
		require.NotPanics(t, it.Close)
	})

	t.Run("it keeps the batch until Close, even after the last row", func(t *testing.T) {
		batch := newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"})
		it := newTestRowsBatchIterator(t, batch)

		require.True(t, it.Next())
		require.False(t, it.Next())
		require.False(t, batch.pooled)
	})

	t.Run("Close releases the batch", func(t *testing.T) {
		batch := newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"})
		it := newTestRowsBatchIterator(t, batch)

		require.True(t, it.Next())
		it.Close()
		require.True(t, batch.pooled)
	})

	t.Run("a second Close does not release the batch again", func(t *testing.T) {
		batch := newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"})
		it := newTestRowsBatchIterator(t, batch)

		it.Close()
		require.NotPanics(t, it.Close)
	})

	t.Run("after Close, Next returns false and At returns the zero row", func(t *testing.T) {
		it := newTestRowsBatchIterator(t, newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"},
		))

		require.True(t, it.Next())
		it.Close()
		require.False(t, it.Next())
		require.Zero(t, *it.At())
	})

	t.Run("the line is a view of the batch, and the release overwrites it", func(t *testing.T) {
		skipWithoutPoisoning(t)
		it := newTestRowsBatchIterator(t, newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"}))

		require.True(t, it.Next())
		line := it.At().line
		require.Equal(t, "one", string(line))

		it.Close()
		require.Equal(t, []byte{0xA5, 0xA5, 0xA5}, line)
	})
}

// rowView is a row that a test can compare. It holds the line as a string, because the line of a
// row is a view that the release of the batch overwrites.
type rowView struct {
	TimestampSec int64
	Line         string
	Metadata     string
	Stream       string
	StreamHash   uint64
	StartsRun    bool
}

// newTestRowsBatchIterator returns an iterator over batch, and closes it when the test ends.
func newTestRowsBatchIterator(t *testing.T, batch *rowsBatch) *rowsBatchIterator {
	t.Helper()

	it := newRowsBatchIterator(batch)
	t.Cleanup(it.Close)
	return &it
}

// drainRows returns every row that the iterator yields.
func drainRows(it *rowsBatchIterator) []rowView {
	var rows []rowView
	for it.Next() {
		currRow := it.At()
		rows = append(rows, rowView{
			TimestampSec: currRow.timestamp / 1e9,
			Line:         string(currRow.line),
			Metadata:     currRow.metadata.String(),
			Stream:       currRow.stream.streamLabels.String(),
			StreamHash:   currRow.stream.streamHash,
			StartsRun:    currRow.startsRun,
		})
	}
	return rows
}
