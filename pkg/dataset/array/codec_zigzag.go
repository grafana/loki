package array

import (
	"context"
	"fmt"
	"unsafe"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/memory"
)

type signed interface{ int32 | int64 }

type zigzagWriter[S signed, U unsigned] struct {
	alloc      *memory.Allocator
	typ        types.Type
	dataWriter Writer
}

func newZigZagWriter(alloc *memory.Allocator, spec Spec, typ types.Type) (Writer, error) {
	if got, want := spec.Kind(), EncodingKindZigZag; got != want {
		return nil, fmt.Errorf("expected spec kind %s, got %s", want, got)
	}

	zzSpec := spec.(*SpecZigZag)
	if zzSpec.Data == nil {
		return nil, fmt.Errorf("zigzag spec must have a data spec")
	}

	switch typ := typ.(type) {
	case *types.Int32:
		return newZigZagWriterTyped[int32, uint32](alloc, zzSpec, typ, &types.Uint32{Nullable: typ.Nullable})
	case *types.Int64:
		return newZigZagWriterTyped[int64, uint64](alloc, zzSpec, typ, &types.Uint64{Nullable: typ.Nullable})
	default:
		return nil, fmt.Errorf("unsupported type %s for zigzag encoding", typ)
	}
}

func newZigZagWriterTyped[S signed, U unsigned](alloc *memory.Allocator, spec *SpecZigZag, typ types.Type, unsignedTyp types.Type) (*zigzagWriter[S, U], error) {
	dataWriter, err := NewWriter(alloc, spec.Data, unsignedTyp)
	if err != nil {
		return nil, fmt.Errorf("creating data writer: %w", err)
	}

	return &zigzagWriter[S, U]{
		alloc:      alloc,
		typ:        typ,
		dataWriter: dataWriter,
	}, nil
}

func (w *zigzagWriter[S, U]) Append(arr columnar.Array) error {
	numArr, ok := arr.(*columnar.Number[S])
	if !ok {
		return fmt.Errorf("expected *columnar.Number[%T], got %T", *new(S), arr)
	}

	values := numArr.Values()

	// Zigzag encode signed values into unsigned.
	shortAlloc := memory.NewAllocator(w.alloc)
	defer shortAlloc.Free()

	encoded := memory.NewBuffer[U](shortAlloc, len(values))
	encoded.Grow(len(values))
	for _, v := range values {
		encoded.Append(zigzagEncode[S, U](v))
	}

	return w.dataWriter.Append(columnar.NewNumber(encoded.Data(), numArr.Validity()))
}

func (w *zigzagWriter[S, U]) Flush(ctx context.Context, sink buffer.Sink) (Array, error) {
	dataArray, err := w.dataWriter.Flush(ctx, sink)
	if err != nil {
		return Array{}, fmt.Errorf("flushing data writer: %w", err)
	}

	return Array{
		Encoding: &EncodingZigZag{},
		Type:     w.typ,
		RowCount: dataArray.RowCount,
		Stats:    dataArray.Stats,
		Children: []Array{dataArray},
	}, nil
}

type zigzagReader[S signed, U unsigned] struct {
	alloc      *memory.Allocator
	arr        Array
	dataReader Reader
}

func newZigZagReader(alloc *memory.Allocator, arr Array, source buffer.Source) (Reader, error) {
	if got, want := arr.Encoding.Kind(), EncodingKindZigZag; got != want {
		return nil, fmt.Errorf("expected encoding kind %s, got %s", want, got)
	}

	if len(arr.Children) != 1 {
		return nil, fmt.Errorf("expected 1 child for zigzag array, got %d", len(arr.Children))
	}

	switch arr.Type.(type) {
	case *types.Int32:
		return newZigZagReaderTyped[int32, uint32](alloc, arr, source)
	case *types.Int64:
		return newZigZagReaderTyped[int64, uint64](alloc, arr, source)
	default:
		return nil, fmt.Errorf("unsupported type %s for zigzag encoding", arr.Type)
	}
}

func newZigZagReaderTyped[S signed, U unsigned](alloc *memory.Allocator, arr Array, source buffer.Source) (*zigzagReader[S, U], error) {
	dataReader, err := NewReader(alloc, arr.Children[0], source)
	if err != nil {
		return nil, fmt.Errorf("creating data reader: %w", err)
	}

	return &zigzagReader[S, U]{
		alloc:      alloc,
		arr:        arr,
		dataReader: dataReader,
	}, nil
}

func (r *zigzagReader[S, U]) Read(ctx context.Context, alloc *memory.Allocator, count int) (columnar.Array, error) {
	if count <= 0 {
		return nil, fmt.Errorf("count must be positive, got %d", count)
	}

	// Read unsigned data with a short-lived allocator; the raw unsigned values
	// don't survive past this call since we decode them into signed values.
	shortAlloc := memory.NewAllocator(alloc)
	defer shortAlloc.Free()

	dataArr, err := r.dataReader.Read(ctx, shortAlloc, count)
	if err != nil {
		return nil, err
	}

	unsignedArr := dataArr.(*columnar.Number[U])
	values := unsignedArr.Values()

	// Zigzag decode unsigned values back to signed.
	decoded := memory.NewBuffer[S](alloc, len(values))

	data := decoded.Next(len(values))

	// Assert length for BCE.
	if len(data) != len(values) {
		panic("decoded buffer length mismatch")
	}
	for i, v := range values {
		data[i] = zigzagDecode[S, U](v)
	}

	// Validity must outlive this call, so clone it with the caller's allocator.
	var validity memory.Bitmap
	if v := unsignedArr.Validity(); v.Len() > 0 {
		validity = *v.Clone(alloc)
	}
	return columnar.NewNumber(decoded.Data(), validity), nil
}

func (r *zigzagReader[S, U]) Reset() {
	r.dataReader.Reset()
}

func (r *zigzagReader[S, U]) Close() error {
	return r.dataReader.Close()
}

func zigzagEncode[S signed, U unsigned](v S) U {
	shift := unsafe.Sizeof(v)*8 - 1
	return U((v << 1) ^ (v >> shift))
}

func zigzagDecode[S signed, U unsigned](v U) S {
	return S(v>>1) ^ -S(v&1)
}
