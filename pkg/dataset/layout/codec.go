package layout

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

// A Writer accumulates data into a [Layout].
type Writer interface {
	// Append appends the data from arr to the Writer. Append returns an error
	// if the data is invalid for the Writer.
	//
	// Implementations must not retain references to arr beyond the call to
	// Append.
	Append(ctx context.Context, arr columnar.Array) error

	// Flush flushes buffered data and returns a new [Layout] that represents
	// the encoded data.
	//
	// After Flush is called, the Writer is reset and ready to accept new data.
	Flush(ctx context.Context) (Layout, error)
}

// NewWriter creates an [Writer] for constructing a Layout.
//
// The provided alloc may be used to allocate memory that is used across the
// entire Writer's lifetime. Callers must ensure that the allocator is valid for
// the entire lifetime of the Writer.
//
// NewWriter returns an error if spec is invalid, or if spec and typ are not
// compatible.
func NewWriter(alloc *memory.Allocator, sink buffer.Sink, spec Spec, typ types.Type) (Writer, error) {
	switch spec.Kind() {
	case KindArray:
		return newArrayWriter(alloc, sink, spec, typ)

	case KindChunked:
		return newChunkedWriter(alloc, sink, spec, typ)

	default:
		return nil, fmt.Errorf("unsupported layout kind %q", spec.Kind())
	}
}

// A Reader reads data from a [Layout].
type Reader interface {
	// Next advances the row range covered by the reader by up to batchSize
	// rows. Next returns the number of rows in the new window.
	//
	// When there are no more rows to read, Next returns 0 and io.EOF.
	//
	// Implementations of Reader should use Next to clear cached data that is no
	// longer reachable after a call to Next.
	Next(ctx context.Context, batchSize int) (int, error)

	// AppendBuffers appends reachable buffers in the current row range to buf.
	// A reachable buffer is any buffer referenced by the filter or projection
	// expression that is not completely masked out by the provided mask.
	AppendBuffers(buf []buffer.ID, filter expr.Expression, projection expr.Expression, mask memory.Bitmap) ([]buffer.ID, error)

	// Prune reads available metadata to compute a high-level mask for the
	// current row range based on the provided expression and mask.
	//
	// The returned mask is intersected with the provided mask and allocated
	// with alloc. Prune returns an error if the mask argument is non-empty and
	// not the same length as the row range.
	Prune(ctx context.Context, alloc *memory.Allocator, expr expr.Expression, mask memory.Bitmap) (memory.Bitmap, error)

	// Filter reads rows to compute a new mask for the current row range based
	// on the provided expression and mask.
	//
	// Filter will cache data for Arrays that survived the filtering so it can
	// be reused by future calls to Filter or Project without re-decoding.
	//
	// The returned mask is intersected with the provided mask and allocated
	// with alloc. Filter returns an error if the mask argument is non-empty and
	// not the same length as the row range.
	Filter(ctx context.Context, alloc *memory.Allocator, expr expr.Expression, mask memory.Bitmap) (memory.Bitmap, error)

	// Project materializes the projection expression for the current row
	// range. The returned array always contains exactly the number of rows
	// in the current window, regardless of the mask.
	//
	// When mask is non-empty, it acts as a selection hint: unselected rows
	// may have undefined values but the array length is unchanged.
	// Callers that need compacted output should apply [compute.Filter] on
	// the result.
	//
	// Rows not currently cached from the Filter stage are read and decoded
	// here. The returned array is allocated with alloc.
	Project(ctx context.Context, alloc *memory.Allocator, projection expr.Expression, mask memory.Bitmap) (columnar.Array, error)

	// Reset rewinds the reader to before the first row, allowing to re-read the
	// data. Reset is safe to call at any point, including after EOF.
	//
	// Implementations may preserve internal caches (e.g., decoded data) across
	// Reset; callers should not assume Reset frees buffers.
	Reset()

	// Close releases any resources held by the Reader.
	Close() error
}

// NewReader creates a [Reader] for a Layout. The provided source is used to
// retrieve buffer data inside the Layout.
//
// The provided alloc may used to allocate memory that is used across the entire
// Reader's lifetime. Callers must ensure that the allocator is valid for the
// entire lifetime of the Reader.
//
// NewReader returns an error if Array specifies an invalid encoding and type
// pair.
func NewReader(alloc *memory.Allocator, layout Layout, source buffer.Source) (Reader, error) {
	switch layout.Kind() {
	case KindArray:
		return newArrayReader(alloc, layout, source)

	case KindChunked:
		return newChunkedReader(alloc, layout, source)

	default:
		return nil, fmt.Errorf("unsupported layout kind %q", layout.Kind())
	}
}
