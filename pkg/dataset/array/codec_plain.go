package array

import (
	"context"
	"errors"
	"fmt"
	"io"
	"unsafe"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataset/types"
	"github.com/grafana/loki/v3/pkg/memory"
)

type plainWriter[T columnar.Numeric] struct {
	alloc *memory.Allocator
	typ   types.Type

	values   memory.Buffer[T]
	validity Writer
	nulls    int
}

func newPlainWriter(alloc *memory.Allocator, spec Spec, typ types.Type) (Writer, error) {
	if got, want := spec.Kind(), EncodingKindPlain; got != want {
		return nil, fmt.Errorf("expected spec kind %s, got %s", want, got)
	}

	plainSpec := spec.(*SpecPlain)

	switch typ := typ.(type) {
	case *types.Int32:
		return newPlainWriterTyped[int32](alloc, plainSpec, typ, typ.Nullable)
	case *types.Int64:
		return newPlainWriterTyped[int64](alloc, plainSpec, typ, typ.Nullable)
	case *types.Uint32:
		return newPlainWriterTyped[uint32](alloc, plainSpec, typ, typ.Nullable)
	case *types.Uint64:
		return newPlainWriterTyped[uint64](alloc, plainSpec, typ, typ.Nullable)
	default:
		return nil, fmt.Errorf("unsupported type %s for plain encoding", typ)
	}
}

func newPlainWriterTyped[T columnar.Numeric](alloc *memory.Allocator, spec *SpecPlain, typ types.Type, nullable bool) (*plainWriter[T], error) {
	hasValidity := spec.Validity != nil
	if nullable != hasValidity {
		return nil, fmt.Errorf("expected %s to have validity %t, got %t", typ, nullable, hasValidity)
	}

	var validityWriter Writer
	if hasValidity {
		var err error
		validityWriter, err = NewWriter(alloc, spec.Validity, &types.Bool{Nullable: false})
		if err != nil {
			return nil, err
		}
	}

	return &plainWriter[T]{
		alloc: alloc,
		typ:   typ,

		values:   memory.NewBuffer[T](alloc, 0),
		validity: validityWriter,
	}, nil
}

func (w *plainWriter[T]) Append(arr columnar.Array) error {
	numArr, ok := arr.(*columnar.Number[T])
	if !ok {
		return fmt.Errorf("expected *columnar.Number[%T], got %T", *new(T), arr)
	}

	values := numArr.Values()
	w.values.Grow(len(values))
	w.values.Append(values...)

	// Fall through to the utility method to handle nulls (including whether our
	// type is not nullable).
	nulls, err := appendNulls(w.alloc, w.validity, numArr, len(values))
	if err != nil {
		return err
	}
	w.nulls += nulls
	return nil
}

func (w *plainWriter[T]) Flush(ctx context.Context, sink Sink) (Array, error) {
	defer w.reset()

	var children []Array
	var validityArray Array
	if validity := w.validity; validity != nil {
		var err error
		validityArray, err = validity.Flush(ctx, sink)
		if err != nil {
			return Array{}, fmt.Errorf("flushing validity writer: %w", err)
		}

		children = append(children, validityArray)
	}

	data := w.values.Serialize()
	bufs, err := sink.WriteBuffers(ctx, []BufferData{data})
	if err != nil {
		return Array{}, fmt.Errorf("writing plain data to a buffer: %w", err)
	}

	return Array{
		Encoding: &EncodingPlain{},
		Type:     w.typ,
		Buffers:  bufs,
		Stats: Stats{
			RowCount:  w.values.Len(),
			NullCount: w.nulls,
		},
		Children: children,
	}, nil
}

func (w *plainWriter[T]) reset() {
	w.values = memory.NewBuffer[T](w.alloc, 0)
	w.nulls = 0
}

type plainReader[T columnar.Numeric] struct {
	alloc  *memory.Allocator
	arr    Array
	source Source

	validity Reader

	initialized bool
	values      []T
	off         int // Offset into values
}

func newPlainReader(alloc *memory.Allocator, arr Array, source Source) (Reader, error) {
	if got, want := arr.Encoding.Kind(), EncodingKindPlain; got != want {
		return nil, fmt.Errorf("expected spec kind %s, got %s", want, got)
	}

	switch typ := arr.Type.(type) {
	case *types.Int32:
		return newPlainReaderTyped[int32](alloc, arr, source, typ.Nullable)
	case *types.Int64:
		return newPlainReaderTyped[int64](alloc, arr, source, typ.Nullable)
	case *types.Uint32:
		return newPlainReaderTyped[uint32](alloc, arr, source, typ.Nullable)
	case *types.Uint64:
		return newPlainReaderTyped[uint64](alloc, arr, source, typ.Nullable)
	default:
		return nil, fmt.Errorf("unsupported type %s for plain encoding", arr.Type)
	}
}

func newPlainReaderTyped[T columnar.Numeric](alloc *memory.Allocator, arr Array, source Source, nullable bool) (*plainReader[T], error) {
	var validityReader Reader
	switch {
	case nullable && len(arr.Children) == 0:
		return nil, errors.New("nullable plain array must have a validity child array")
	case nullable && len(arr.Children) > 1:
		return nil, fmt.Errorf("expected 1 child for nullable plain array, got %d", len(arr.Children))
	case !nullable && len(arr.Children) != 0:
		return nil, fmt.Errorf("expected 0 children for non-nullable plain array, got %d", len(arr.Children))
	}
	if nullable {
		var err error
		validityReader, err = NewReader(alloc, arr.Children[0], source)
		if err != nil {
			return nil, fmt.Errorf("creating validity reader: %w", err)
		}
	}

	return &plainReader[T]{
		alloc:  alloc,
		arr:    arr,
		source: source,

		validity: validityReader,
	}, nil
}

func (r *plainReader[T]) Read(ctx context.Context, alloc *memory.Allocator, count int) (columnar.Array, error) {
	if !r.initialized {
		// We use the reader's allocator for initializing since the data
		// persists across calls to Read.
		if err := r.init(ctx, r.alloc); err != nil {
			return nil, err
		}
		r.initialized = true
	}

	endOff := min(r.off+count, len(r.values))
	if endOff-r.off == 0 {
		return nil, io.EOF
	}
	values := r.values[r.off:endOff:endOff]

	// Read validity bytes now.
	var validity memory.Bitmap
	if r.validity != nil {
		validityArr, err := r.validity.Read(ctx, alloc, count)
		if err != nil {
			return nil, fmt.Errorf("reading validity: %w", err)
		}

		validityBoolArr := validityArr.(*columnar.Bool)
		validity = validityBoolArr.Values()
	}

	r.off += len(values)
	return columnar.NewNumber(values, validity), nil
}

func (r *plainReader[T]) init(ctx context.Context, alloc *memory.Allocator) error {
	data, err := r.source.ReadBuffers(ctx, alloc, r.arr.Buffers)
	if err != nil {
		return fmt.Errorf("fetching buffer data: %w", err)
	} else if len(data) != 1 {
		return fmt.Errorf("expected 1 buffer, got %d", len(data))
	}

	if len(data[0]) > 0 {
		r.values = unsafe.Slice((*T)(unsafe.Pointer(unsafe.SliceData(data[0]))), r.arr.Stats.RowCount)
	}
	return nil
}

func (r *plainReader[T]) Close() error {
	if r.validity != nil {
		return r.validity.Close()
	}
	return nil
}
