package layout

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

type chunkedWriter struct {
	spec  *SpecChunked
	typ   types.Type
	inner Writer

	chunks []Layout

	currBytes int // Number of bytes in the current chunk.
	currRows  int // Number of rows in the current chunk.
}

func newChunkedWriter(alloc *memory.Allocator, sink buffer.Sink, spec Spec, typ types.Type) (*chunkedWriter, error) {
	if got, want := spec.Kind(), KindChunked; got != want {
		return nil, fmt.Errorf("invalid spec kind for chunked writer: got %s, want %s", got, want)
	}

	chunkSpec := spec.(*SpecChunked)
	innerWriter, err := NewWriter(alloc, sink, chunkSpec.Chunk, typ)
	if err != nil {
		return nil, fmt.Errorf("creating inner writer: %w", err)
	}

	return &chunkedWriter{
		spec:  chunkSpec,
		typ:   typ,
		inner: innerWriter,
	}, nil
}

func (w *chunkedWriter) Append(ctx context.Context, arr columnar.Array) error {
	for arr.Len() > 0 {
		if w.needFlush(arr) {
			if err := w.flushChunk(ctx); err != nil {
				return fmt.Errorf("flushing full chunk: %w", err)
			}
		}

		n := w.fitRows(arr)
		sub := arr.Slice(0, n)
		if err := w.inner.Append(ctx, sub); err != nil {
			return fmt.Errorf("appending to inner writer: %w", err)
		}

		w.currBytes += sub.Size()
		w.currRows += sub.Len()
		arr = arr.Slice(n, arr.Len())
	}
	return nil
}

func (w *chunkedWriter) needFlush(arr columnar.Array) bool {
	switch {
	case w.currBytes > 0 && w.spec.SizeHint > 0 && w.currBytes+arr.Size() >= w.spec.SizeHint:
		return true
	case w.currRows > 0 && w.spec.MaxRowCount > 0 && w.currRows+arr.Len() >= w.spec.MaxRowCount:
		return true
	}
	return false
}

func (w *chunkedWriter) fitRows(arr columnar.Array) int {
	n := arr.Len()

	if maxRows := w.spec.MaxRowCount; maxRows > 0 {
		if remaining := maxRows - w.currRows; remaining < n {
			n = remaining
		}
	}

	if sizeHint := w.spec.SizeHint; sizeHint > 0 {
		remaining := sizeHint - w.currBytes
		if remaining > 0 && arr.Slice(0, n).Size() > remaining {
			fit := sort.Search(n, func(i int) bool {
				return arr.Slice(0, i+1).Size() > remaining
			})
			if fit < n {
				n = fit
			}
		}
	}

	return max(n, 1) // Always make progress.
}

func (w *chunkedWriter) flushChunk(ctx context.Context) error {
	// Flush the current chunk and start a new one.
	l, err := w.inner.Flush(ctx)
	if err != nil {
		return fmt.Errorf("flushing full chunk: %w", err)
	}

	w.chunks = append(w.chunks, l)
	w.currBytes = 0
	w.currRows = 0
	return nil
}

func (w *chunkedWriter) Flush(ctx context.Context) (Layout, error) {
	defer w.reset()

	if w.currRows > 0 {
		if err := w.flushChunk(ctx); err != nil {
			return nil, fmt.Errorf("flushing final chunk: %w", err)
		}
	}

	return &Chunked{
		Type:   w.typ,
		Chunks: w.chunks,
	}, nil
}

func (w *chunkedWriter) reset() {
	w.chunks = nil
	w.currBytes = 0
	w.currRows = 0
}

type chunkedReader struct {
	layout *Chunked
	source buffer.Source
	alloc  *memory.Allocator

	// chunkEnds[i] is the exclusive end row of chunk i.
	chunkEnds []int
	totalRows int

	// Active inner readers for chunks overlapping the current window.
	// Sorted by chunk index. Created lazily, closed when Next() advances
	// fully past them.
	active []activeChunk

	// Current window.
	offset int // Global row offset of current window start.
	length int // Number of rows in current window.
}

type activeChunk struct {
	idx    int               // Index into layout.Chunks.
	reader Reader            // Inner reader.
	alloc  *memory.Allocator // Child allocator; freed when chunk is closed.
}

func newChunkedReader(alloc *memory.Allocator, layout Layout, source buffer.Source) (*chunkedReader, error) {
	if got, want := layout.Kind(), KindChunked; got != want {
		return nil, fmt.Errorf("invalid layout kind for chunked reader: got %s, want %s", got, want)
	}

	cl := layout.(*Chunked)

	chunkEnds := make([]int, len(cl.Chunks))
	total := 0
	for i, chunk := range cl.Chunks {
		total += chunk.Len()
		chunkEnds[i] = total
	}

	return &chunkedReader{
		layout:    cl,
		source:    source,
		alloc:     alloc,
		chunkEnds: chunkEnds,
		totalRows: total,
	}, nil
}

func (r *chunkedReader) Next(ctx context.Context, batchSize int) (int, error) {
	r.offset += r.length

	if r.offset >= r.totalRows {
		r.length = 0
		return 0, io.EOF
	}

	r.length = min(batchSize, r.totalRows-r.offset)

	r.closePassedChunks()
	if err := r.activateChunks(); err != nil {
		return 0, fmt.Errorf("activating chunks: %w", err)
	}
	if err := r.advanceInnerReaders(ctx); err != nil {
		return 0, fmt.Errorf("advancing inner readers: %w", err)
	}

	return r.length, nil
}

// closePassedChunks closes and frees inner readers for chunks the window has
// fully moved past.
func (r *chunkedReader) closePassedChunks() {
	var n int
	for _, ac := range r.active {
		if r.chunkEnds[ac.idx] <= r.offset {
			ac.reader.Close()

			// Free the child allocator; the reader is done with it.
			//
			// Implementations of Read aren't allowed to use their allocator for
			// any memory they return on Prune/Filter/Project, so any memory
			// they allocated with their alloc is stale and can be reclaimed
			// safely.
			ac.alloc.Free()
			continue
		}
		r.active[n] = ac
		n++
	}

	clear(r.active[n:]) // Clear references to closed readers.
	r.active = r.active[:n]
}

// activateChunks creates inner readers for chunks that newly overlap with the
// current window.
func (r *chunkedReader) activateChunks() error {
	var (
		first = sort.Search(len(r.chunkEnds), func(i int) bool { return r.chunkEnds[i] > r.offset })
		last  = sort.Search(len(r.chunkEnds), func(i int) bool { return r.chunkEnds[i] >= r.offset+r.length })
	)

	// New chunks always come after existing active chunks.
	newStart := first
	if len(r.active) > 0 {
		newStart = r.active[len(r.active)-1].idx + 1
	}

	for i := newStart; i <= last; i++ {
		// Create a child allocator that will live for the lifetime of the child
		// Reader, freed once we've fully moved past the chunk.
		childAlloc := memory.NewAllocator(r.alloc)
		reader, err := NewReader(childAlloc, r.layout.Chunks[i], r.source)
		if err != nil {
			childAlloc.Free()
			return fmt.Errorf("creating reader for chunk %d: %w", i, err)
		}
		r.active = append(r.active, activeChunk{
			idx:    i,
			reader: reader,
			alloc:  childAlloc,
		})
	}

	return nil
}

// advanceInnerReaders calls Next on each active inner reader with the
// sub-window size for the current outer window.
func (r *chunkedReader) advanceInnerReaders(ctx context.Context) error {
	for _, ac := range r.active {
		var (
			start = max(r.offset, r.chunkStart(ac.idx))
			end   = min(r.offset+r.length, r.chunkEnds[ac.idx])

			chunkSize = end - start
		)

		if _, err := ac.reader.Next(ctx, chunkSize); err != nil {
			return fmt.Errorf("advancing chunk %d: %w", ac.idx, err)
		}
	}

	return nil
}

// chunkStart returns the first global row index of chunk i.
func (r *chunkedReader) chunkStart(i int) int {
	if i == 0 {
		return 0
	}
	return r.chunkEnds[i-1]
}

func (r *chunkedReader) AppendBuffers(buf []buffer.ID, filter expr.Expression, projection expr.Expression, mask memory.Bitmap) ([]buffer.ID, error) {
	for _, ac := range r.active {
		subMask := r.sliceMask(mask, ac.idx)

		var err error
		buf, err = ac.reader.AppendBuffers(buf, filter, projection, subMask)
		if err != nil {
			return buf, fmt.Errorf("chunk %d: %w", ac.idx, err)
		}
	}

	return buf, nil
}

// sliceMask returns the portion of mask corresponding to the given chunk's
// sub-window. Returns a zero Bitmap if mask is empty (all-selected).
func (r *chunkedReader) sliceMask(mask memory.Bitmap, chunkIdx int) memory.Bitmap {
	if mask.Len() == 0 {
		return memory.Bitmap{}
	}

	var (
		start = max(r.offset, r.chunkStart(chunkIdx)) - r.offset
		end   = min(r.offset+r.length, r.chunkEnds[chunkIdx]) - r.offset
	)
	return *mask.Slice(start, end)
}

func (r *chunkedReader) Prune(ctx context.Context, alloc *memory.Allocator, e expr.Expression, mask memory.Bitmap) (memory.Bitmap, error) {
	if mask.Len() != 0 && mask.Len() != r.length {
		return memory.Bitmap{}, fmt.Errorf("mask length %d does not match row range %d", mask.Len(), r.length)
	}

	if len(r.active) == 1 {
		subMask := r.sliceMask(mask, r.active[0].idx)
		return r.active[0].reader.Prune(ctx, alloc, e, subMask)
	}

	// Create a short-lived allocator for the various prunes, which don't live
	// past the end of this call (thanks to building a single result bitmap).
	partAlloc := memory.NewAllocator(alloc)
	defer partAlloc.Free()

	result := memory.NewBitmap(alloc, r.length)
	for _, ac := range r.active {
		subMask := r.sliceMask(mask, ac.idx)
		subResult, err := ac.reader.Prune(ctx, partAlloc, e, subMask)
		if err != nil {
			return memory.Bitmap{}, fmt.Errorf("chunk %d: %w", ac.idx, err)
		}
		result.AppendBitmap(subResult)
	}
	return result, nil
}

func (r *chunkedReader) Filter(ctx context.Context, alloc *memory.Allocator, e expr.Expression, mask memory.Bitmap) (memory.Bitmap, error) {
	if mask.Len() != 0 && mask.Len() != r.length {
		return memory.Bitmap{}, fmt.Errorf("mask length %d does not match row range %d", mask.Len(), r.length)
	}

	if len(r.active) == 1 {
		subMask := r.sliceMask(mask, r.active[0].idx)
		return r.active[0].reader.Filter(ctx, alloc, e, subMask)
	}

	// Create a short-lived allocator for the various filters, which don't
	// live past the end of this call (thanks to building a single result
	// bitmap).
	partAlloc := memory.NewAllocator(alloc)
	defer partAlloc.Free()

	result := memory.NewBitmap(alloc, r.length)
	for _, ac := range r.active {
		subMask := r.sliceMask(mask, ac.idx)
		subResult, err := ac.reader.Filter(ctx, partAlloc, e, subMask)
		if err != nil {
			return memory.Bitmap{}, fmt.Errorf("chunk %d: %w", ac.idx, err)
		}
		result.AppendBitmap(subResult)
	}
	return result, nil
}

func (r *chunkedReader) Project(ctx context.Context, alloc *memory.Allocator, projection expr.Expression, mask memory.Bitmap) (columnar.Array, error) {
	if len(r.active) == 1 {
		subMask := r.sliceMask(mask, r.active[0].idx)
		return r.active[0].reader.Project(ctx, alloc, projection, subMask)
	}

	// Create a short-lived allocator for the various projections, which don't
	// live past the end of this call (thanks to the Concat).
	partAlloc := memory.NewAllocator(alloc)
	defer partAlloc.Free()

	parts := make([]columnar.Array, 0, len(r.active))
	for _, ac := range r.active {
		subMask := r.sliceMask(mask, ac.idx)
		part, err := ac.reader.Project(ctx, partAlloc, projection, subMask)
		if err != nil {
			return nil, fmt.Errorf("chunk %d: %w", ac.idx, err)
		}
		parts = append(parts, part)
	}
	return columnar.Concat(alloc, parts)
}

func (r *chunkedReader) Reset() {
	// Close and free any active chunks. This is inefficient if we're already on
	// the first chunk, but since this is a rare case (it's more typical to
	// Reset after EOF) it's not worth optimizing for.
	for _, ac := range r.active {
		ac.reader.Close()
		ac.alloc.Free()
	}

	r.active = r.active[:0]
	r.offset = 0
	r.length = 0
}

func (r *chunkedReader) Close() error {
	r.Reset()
	r.active = nil
	return nil
}
