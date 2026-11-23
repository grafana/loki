package dataset

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestChunkCursor(t *testing.T) {
	var alloc memory.Allocator

	t.Run("returns the chunk and the index in the chunk of ascending rows", func(t *testing.T) {
		view := viewOf(&alloc, []string{"a", "b"}, []string{"c"}, []string{"d", "e"})
		cursor := newChunkCursor(view)

		for _, tc := range []struct {
			row       int
			wantChunk int
			wantIndex int
		}{
			{row: 0, wantChunk: 0, wantIndex: 0},
			{row: 1, wantChunk: 0, wantIndex: 1},
			{row: 2, wantChunk: 1, wantIndex: 0},
			{row: 4, wantChunk: 2, wantIndex: 1},
		} {
			chunk, index := cursor.locate(tc.row)
			require.Same(t, view.Chunk(tc.wantChunk), chunk, "row %d", tc.row)
			require.Equal(t, tc.wantIndex, index, "row %d", tc.row)
		}
	})

	t.Run("skips the chunks that hold no requested row", func(t *testing.T) {
		view := viewOf(&alloc, []string{"a", "b"}, []string{"c"}, []string{"d", "e"})
		cursor := newChunkCursor(view)

		_, _ = cursor.locate(0)
		chunk, index := cursor.locate(4)

		require.Same(t, view.Chunk(2), chunk)
		require.Equal(t, 1, index)
	})

	t.Run("panics when the view has no chunk", func(t *testing.T) {
		cursor := newChunkCursor(&columnar.ChunkedUTF8{})

		require.PanicsWithValue(t, "dataset: row 0 is past the end of the view, which has 0 rows", func() { cursor.locate(0) })
	})

	t.Run("panics when the row is at the end of the view", func(t *testing.T) {
		cursor := newChunkCursor(viewOf(&alloc, []string{"a"}, []string{"b"}))

		require.PanicsWithValue(t, "dataset: row 2 is past the end of the view, which has 2 rows", func() { cursor.locate(2) })
	})

	t.Run("panics when the row is in a chunk before the current chunk", func(t *testing.T) {
		cursor := newChunkCursor(viewOf(&alloc, []string{"a", "b"}, []string{"c"}))

		_, _ = cursor.locate(2)

		require.PanicsWithValue(t, "dataset: row 0 is before the current chunk, which starts at row 2", func() { cursor.locate(0) })
	})
}
