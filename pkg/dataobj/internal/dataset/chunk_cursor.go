package dataset

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
)

// A chunkCursor finds the chunk of a [columnar.ChunkedUTF8] that holds a row.
// It moves forward only, so it serves callers that ask for ascending rows.
type chunkCursor struct {
	view *columnar.ChunkedUTF8
	k    int // Index of the current chunk.
	base int // Row of the first element of the current chunk.
}

func newChunkCursor(view *columnar.ChunkedUTF8) chunkCursor {
	return chunkCursor{view: view}
}

// locate returns the chunk that holds row i, and the index of i in that chunk.
// A call must not ask for a row in a chunk before the chunk of an earlier call.
// locate panics when a call breaks this rule, and when i is at or past the end
// of the view.
func (c *chunkCursor) locate(i int) (chunk *columnar.UTF8, index int) {
	if i < c.base {
		panic(fmt.Sprintf("dataset: row %d is before the current chunk, which starts at row %d", i, c.base))
	}

	for c.k < c.view.NumChunks() {
		chunk = c.view.Chunk(c.k)
		if i < c.base+chunk.Len() {
			return chunk, i - c.base
		}
		c.base += chunk.Len()
		c.k++
	}
	panic(fmt.Sprintf("dataset: row %d is past the end of the view, which has %d rows", i, c.base))
}
