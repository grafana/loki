package layout

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

type arrayWriter struct {
	inner array.Writer
	sink  buffer.Sink
}

func newArrayWriter(alloc *memory.Allocator, sink buffer.Sink, spec Spec, typ types.Type) (*arrayWriter, error) {
	if got, want := spec.Kind(), KindArray; got != want {
		return nil, fmt.Errorf("invalid spec kind for array writer: got %s, want %s", got, want)
	}

	arrSpec := spec.(*SpecArray)

	innerWriter, err := array.NewWriter(alloc, arrSpec.Spec, typ)
	if err != nil {
		return nil, fmt.Errorf("creating inner writer: %w", err)
	}

	return &arrayWriter{
		inner: innerWriter,
		sink:  sink,
	}, nil
}

func (w *arrayWriter) Append(_ context.Context, arr columnar.Array) error {
	return w.inner.Append(arr)
}

func (w *arrayWriter) Flush(ctx context.Context) (Layout, error) {
	arr, err := w.inner.Flush(ctx, w.sink)
	if err != nil {
		return nil, fmt.Errorf("flushing array: %w", err)
	}

	return &Array{Metadata: arr}, nil
}

type arrayReader struct {
	layout *Array
	source buffer.Source
	alloc  *memory.Allocator

	// Lazily loaded full array. Loaded once on first Filter/Project call.
	data   columnar.Array
	loaded bool

	// Window managed by Next.
	totalRows int // Total number of rows in data.
	offset    int // Offset into data of the first row in the current window.
	length    int // Number of rows in the current window.
}

func newArrayReader(alloc *memory.Allocator, layout Layout, source buffer.Source) (*arrayReader, error) {
	if got, want := layout.Kind(), KindArray; got != want {
		return nil, fmt.Errorf("invalid layout kind for array reader: got %s, want %s", got, want)
	}

	arrLayout := layout.(*Array)

	return &arrayReader{
		layout:    arrLayout,
		source:    source,
		alloc:     alloc,
		totalRows: arrLayout.Metadata.RowCount,
	}, nil
}

func (r *arrayReader) Next(_ context.Context, batchSize int) (int, error) {
	r.offset += r.length

	if r.offset >= r.totalRows {
		r.length = 0
		return 0, io.EOF
	}

	r.length = min(batchSize, r.totalRows-r.offset)
	return r.length, nil
}

func (r *arrayReader) AppendBuffers(buf []buffer.ID, _ expr.Expression, _ expr.Expression, _ memory.Bitmap) ([]buffer.ID, error) {
	return appendArrayBuffers(buf, &r.layout.Metadata), nil
}

// appendArrayBuffers recursively collects all Buffer references from an
// array.Array metadata tree.
func appendArrayBuffers(buf []buffer.ID, arr *array.Array) []buffer.ID {
	buf = append(buf, arr.Buffers...)
	for i := range arr.Children {
		buf = appendArrayBuffers(buf, &arr.Children[i])
	}
	return buf
}

func (r *arrayReader) Prune(_ context.Context, _ *memory.Allocator, _ expr.Expression, mask memory.Bitmap) (memory.Bitmap, error) {
	if mask.Len() != 0 && mask.Len() != r.length {
		return memory.Bitmap{}, fmt.Errorf("mask length %d does not match row range %d", mask.Len(), r.length)
	}
	// TODO(rfratto): Use Stats for pruning (min/max when available).
	return mask, nil
}

func (r *arrayReader) Filter(ctx context.Context, alloc *memory.Allocator, e expr.Expression, mask memory.Bitmap) (memory.Bitmap, error) {
	if err := r.loadData(ctx); err != nil {
		return mask, err
	}

	windowData := r.data.Slice(r.offset, r.offset+r.length)
	result, err := expr.Evaluate(alloc, e, windowData, mask)
	if err != nil {
		return mask, fmt.Errorf("evaluating filter expression: %w", err)
	}

	resultMask, err := datumToMask(alloc, result, r.length)
	if err != nil {
		return mask, fmt.Errorf("converting filter result to mask: %w", err)
	}
	if mask.Len() > 0 {
		// datumToMask returns the raw values bitmap, which has undefined
		// bits at unselected positions. Intersect with the input mask so
		// only selected-and-matched rows survive.
		resultMask = intersectMasks(alloc, resultMask, mask)
	}
	return resultMask, nil
}

func (r *arrayReader) loadData(ctx context.Context) error {
	if r.loaded {
		return nil
	}

	inner, err := array.NewReader(r.alloc, r.layout.Metadata, r.source)
	if err != nil {
		return fmt.Errorf("creating array reader: %w", err)
	}
	defer inner.Close()

	// We read the full array into memory here, which can be slightly wasteful,
	// but it is generally faster as it minimizes decoder reentrance.
	data, err := inner.Read(ctx, r.alloc, r.totalRows)
	if err != nil && err != io.EOF {
		return fmt.Errorf("reading array data: %w", err)
	}

	r.data = data
	r.loaded = true
	return nil
}

func (r *arrayReader) Project(ctx context.Context, alloc *memory.Allocator, projection expr.Expression, mask memory.Bitmap) (columnar.Array, error) {
	if err := r.loadData(ctx); err != nil {
		return nil, err
	}

	windowData := r.data.Slice(r.offset, r.offset+r.length)

	projected, err := expr.Evaluate(alloc, projection, windowData, mask)
	if err != nil {
		return nil, fmt.Errorf("evaluating projection: %w", err)
	}

	// expr.Evaluate may return its input unchanged (e.g., Identity
	// projection). If the final result still aliases r.data, clone into
	// alloc so the caller owns the memory.
	final := projected
	if final == windowData {
		var err error
		final, err = columnar.Clone(alloc, final)
		if err != nil {
			return nil, fmt.Errorf("cloning result: %w", err)
		}
	}

	result, ok := final.(columnar.Array)
	if !ok {
		return nil, fmt.Errorf("projection produced %T, expected Array", final)
	}
	return result, nil
}

func (r *arrayReader) Reset() {
	r.offset = 0
	r.length = 0
}

func (r *arrayReader) Close() error {
	r.Reset()

	r.loaded = false
	r.data = nil
	return nil
}
