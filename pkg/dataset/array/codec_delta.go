package array

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/memory"
)

type integer interface {
	int32 | int64 | uint32 | uint64
}

type deltaWriter[T integer] struct {
	alloc *memory.Allocator
	typ   types.Type
	next  Writer
	last  T
}

func newDeltaWriter(alloc *memory.Allocator, spec Spec, typ types.Type) (Writer, error) {
	if got, want := spec.Kind(), EncodingKindDelta; got != want {
		return nil, fmt.Errorf("expected spec kind %s, got %s", want, got)
	}

	dSpec := spec.(*SpecDelta)
	if dSpec.Data == nil {
		return nil, fmt.Errorf("delta spec must have a data spec")
	}

	switch typ.(type) {
	case *types.Int32:
		return newDeltaWriterTyped[int32](alloc, dSpec, typ)
	case *types.Int64:
		return newDeltaWriterTyped[int64](alloc, dSpec, typ)
	case *types.Uint32:
		return newDeltaWriterTyped[uint32](alloc, dSpec, typ)
	case *types.Uint64:
		return newDeltaWriterTyped[uint64](alloc, dSpec, typ)
	default:
		return nil, fmt.Errorf("unsupported type %s for delta encoding", typ)
	}
}

func newDeltaWriterTyped[T integer](alloc *memory.Allocator, spec *SpecDelta, typ types.Type) (*deltaWriter[T], error) {
	next, err := NewWriter(alloc, spec.Data, typ)
	if err != nil {
		return nil, fmt.Errorf("creating data writer: %w", err)
	}

	return &deltaWriter[T]{
		alloc: alloc,
		typ:   typ,
		next:  next,
	}, nil
}

func (w *deltaWriter[T]) Append(arr columnar.Array) error {
	numArr, ok := arr.(*columnar.Number[T])
	if !ok {
		return fmt.Errorf("expected *columnar.Number[%T], got %T", *new(T), arr)
	}

	values := numArr.Values()

	shortAlloc := memory.NewAllocator(w.alloc)
	defer shortAlloc.Free()

	encoded := memory.NewBuffer[T](shortAlloc, len(values))
	encoded.Grow(len(values))
	for _, v := range values {
		encoded.Append(v - w.last)
		w.last = v
	}

	return w.next.Append(columnar.NewNumber(encoded.Data(), numArr.Validity()))
}

func (w *deltaWriter[T]) Flush(ctx context.Context, sink buffer.Sink) (Array, error) {
	defer w.reset()

	dataArray, err := w.next.Flush(ctx, sink)
	if err != nil {
		return Array{}, fmt.Errorf("flushing data writer: %w", err)
	}

	return Array{
		Encoding: &EncodingDelta{},
		Type:     w.typ,
		RowCount: dataArray.RowCount,
		Stats:    dataArray.Stats,
		Children: []Array{dataArray},
	}, nil
}

func (w *deltaWriter[T]) reset() {
	w.last = 0
}

type deltaReader[T integer] struct {
	alloc      *memory.Allocator
	arr        Array
	dataReader Reader
	last       T
}

func newDeltaReader(alloc *memory.Allocator, arr Array, source buffer.Source) (Reader, error) {
	if got, want := arr.Encoding.Kind(), EncodingKindDelta; got != want {
		return nil, fmt.Errorf("expected encoding kind %s, got %s", want, got)
	}

	if len(arr.Children) != 1 {
		return nil, fmt.Errorf("expected 1 child for delta array, got %d", len(arr.Children))
	}

	switch arr.Type.(type) {
	case *types.Int32:
		return newDeltaReaderTyped[int32](alloc, arr, source)
	case *types.Int64:
		return newDeltaReaderTyped[int64](alloc, arr, source)
	case *types.Uint32:
		return newDeltaReaderTyped[uint32](alloc, arr, source)
	case *types.Uint64:
		return newDeltaReaderTyped[uint64](alloc, arr, source)
	default:
		return nil, fmt.Errorf("unsupported type %s for delta encoding", arr.Type)
	}
}

func newDeltaReaderTyped[T integer](alloc *memory.Allocator, arr Array, source buffer.Source) (*deltaReader[T], error) {
	dataReader, err := NewReader(alloc, arr.Children[0], source)
	if err != nil {
		return nil, fmt.Errorf("creating data reader: %w", err)
	}

	return &deltaReader[T]{
		alloc:      alloc,
		arr:        arr,
		dataReader: dataReader,
	}, nil
}

func (r *deltaReader[T]) Read(ctx context.Context, alloc *memory.Allocator, count int) (columnar.Array, error) {
	if count <= 0 {
		return nil, fmt.Errorf("count must be positive, got %d", count)
	}

	// Read deltas with a short-lived allocator; the raw deltas don't survive
	// past this call since we decode them into absolute values.
	shortAlloc := memory.NewAllocator(alloc)
	defer shortAlloc.Free()

	dataArr, err := r.dataReader.Read(ctx, shortAlloc, count)
	if err != nil {
		return nil, err
	}

	deltaArr := dataArr.(*columnar.Number[T])
	deltas := deltaArr.Values()

	decoded := memory.NewBuffer[T](alloc, len(deltas))
	data := decoded.Next(len(deltas))

	last := r.last
	{
		// Check length for BCE.
		if len(data) != len(deltas) {
			panic("decoded buffer length mismatch")
		}
		for i, d := range deltas {
			last += d
			data[i] = last
		}
	}
	r.last = last

	// Validity must outlive this call, so clone it with the caller's allocator.
	var validity memory.Bitmap
	if v := deltaArr.Validity(); v.Len() > 0 {
		validity = *v.Clone(alloc)
	}
	return columnar.NewNumber(decoded.Data(), validity), nil
}

func (r *deltaReader[T]) Reset() {
	r.resetSelf()
	r.dataReader.Reset()
}

func (r *deltaReader[T]) resetSelf() {
	// Reset the offset but keep everything else to allow for re-reading data.
	r.last = 0
}

func (r *deltaReader[T]) Close() error {
	r.resetSelf()
	return r.dataReader.Close()
}
