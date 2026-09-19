package array

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/memory"
)

type binaryWriter struct {
	alloc *memory.Allocator
	typ   *types.UTF8

	offsetWriter Writer
	validity     Writer

	initialized bool
	data        memory.Buffer[byte]
	nulls       int
	rows        int
}

func newBinaryWriter(alloc *memory.Allocator, spec Spec, typ types.Type) (Writer, error) {
	if got, want := spec.Kind(), EncodingKindBinary; got != want {
		return nil, fmt.Errorf("expected spec kind %s, got %s", want, got)
	} else if got, want := typ.Kind(), types.KindUTF8; got != want {
		return nil, fmt.Errorf("expected type %s, got %s", want, got)
	}

	var (
		binarySpec = spec.(*SpecBinary)
		utf8Typ    = typ.(*types.UTF8)
	)

	if binarySpec.Offsets == nil {
		return nil, errors.New("binary spec requires an offsets spec")
	}

	hasValidity := binarySpec.Validity != nil
	if utf8Typ.Nullable != hasValidity {
		return nil, fmt.Errorf("expected %s to have validity %t, got %t", utf8Typ, utf8Typ.Nullable, hasValidity)
	}

	offsetWriter, err := NewWriter(alloc, binarySpec.Offsets, &types.Int32{Nullable: false})
	if err != nil {
		return nil, fmt.Errorf("creating offset writer: %w", err)
	}

	var validityWriter Writer
	if hasValidity {
		validityWriter, err = NewWriter(alloc, binarySpec.Validity, &types.Bool{Nullable: false})
		if err != nil {
			return nil, err
		}
	}

	return &binaryWriter{
		alloc: alloc,
		typ:   utf8Typ,

		data:         memory.NewBuffer[byte](alloc, 0),
		offsetWriter: offsetWriter,
		validity:     validityWriter,
	}, nil
}

func (w *binaryWriter) Append(arr columnar.Array) error {
	utf8Arr, ok := arr.(*columnar.UTF8)
	if !ok {
		return fmt.Errorf("expected *columnar.UTF8, got %T", arr)
	}

	if !w.initialized {
		w.init()
		w.initialized = true
	}

	var (
		baseOffset = int32(w.data.Len())

		srcData    = utf8Arr.Data()
		srcOffsets = utf8Arr.Offsets()
		dataStart  = srcOffsets[0]
		dataEnd    = srcOffsets[len(srcOffsets)-1]
	)

	// Validate before any mutation so a failed Append leaves the writer's
	// state unchanged.
	if err := validateNulls(w.validity, utf8Arr, utf8Arr.Len()); err != nil {
		return err
	}

	// Fall through to the utility method to handle nulls (including whether our
	// type is not nullable).
	nulls, err := appendNulls(w.alloc, w.validity, utf8Arr, utf8Arr.Len())
	if err != nil {
		return err
	}
	w.nulls += nulls

	// Append only the referenced data range. Sliced UTF8 arrays share the
	// full parent data buffer, so srcData may contain bytes outside the
	// offset range.
	w.data.Grow(int(dataEnd - dataStart))
	w.data.Append(srcData[dataStart:dataEnd]...)

	// Build rebased offsets and write through the child offset writer (skip
	// first since the leading zero was written by init).
	{
		alloc := memory.NewAllocator(w.alloc)
		defer alloc.Free()

		rebasedBuilder := columnar.NewNumberBuilder[int32](alloc)
		rebasedBuilder.Grow(utf8Arr.Len())

		for i := range utf8Arr.Len() {
			rebasedBuilder.AppendValue(srcOffsets[i+1] - dataStart + baseOffset)
		}

		rebased := rebasedBuilder.Build()
		if err := w.offsetWriter.Append(rebased); err != nil {
			return fmt.Errorf("appending offsets: %w", err)
		}
		w.rows += utf8Arr.Len()
	}
	return nil
}

func (w *binaryWriter) init() {
	// Write the leading zero offset, since the offset array has N+1 values.
	leading := columnar.NewNumber([]int32{0}, memory.Bitmap{})

	// offsetWriter.Append cannot fail for a single zero value written to a
	// fresh writer, so we intentionally ignore the error.
	_ = w.offsetWriter.Append(leading)
}

func (w *binaryWriter) Flush(ctx context.Context, sink buffer.Sink) (Array, error) {
	defer w.reset()

	// Write data buffer.
	data := w.data.Serialize()
	bufs, err := sink.WriteBuffers(ctx, []buffer.Data{data})
	if err != nil {
		return Array{}, fmt.Errorf("writing binary data to a buffer: %w", err)
	}

	var children []Array
	var offsetArray Array
	offsetArray, err = w.offsetWriter.Flush(ctx, sink)
	if err != nil {
		return Array{}, fmt.Errorf("flushing offset writer: %w", err)
	}
	children = append(children, offsetArray)

	var validityArray Array
	if validity := w.validity; validity != nil {
		var err error
		validityArray, err = validity.Flush(ctx, sink)
		if err != nil {
			return Array{}, fmt.Errorf("flushing validity writer: %w", err)
		}

		children = append(children, validityArray)
	}

	return Array{
		Encoding: &EncodingBinary{},
		Type:     w.typ,
		Buffers:  bufs,
		RowCount: w.rows,
		Stats: Stats{
			NullCount: w.nulls,
		},
		Children: children,
	}, nil
}

func (w *binaryWriter) reset() {
	w.initialized = false
	w.data = memory.NewBuffer[byte](w.alloc, 0)
	w.nulls = 0
	w.rows = 0
}

type binaryReader struct {
	alloc  *memory.Allocator
	arr    Array
	source buffer.Source

	offsets  Reader
	validity Reader

	initialized bool
	data        []byte
	offsetData  []int32
	off         int // Row offset into data
}

func newBinaryReader(alloc *memory.Allocator, arr Array, source buffer.Source) (*binaryReader, error) {
	if got, want := arr.Encoding.Kind(), EncodingKindBinary; got != want {
		return nil, fmt.Errorf("expected spec kind %s, got %s", want, got)
	} else if got, want := arr.Type.Kind(), types.KindUTF8; got != want {
		return nil, fmt.Errorf("expected type %s, got %s", want, got)
	}

	var (
		utf8Typ = arr.Type.(*types.UTF8)
	)

	switch {
	case !utf8Typ.Nullable && len(arr.Children) != 1:
		return nil, fmt.Errorf("expected 1 child for non-nullable binary array, got %d", len(arr.Children))
	case utf8Typ.Nullable && len(arr.Children) != 2:
		return nil, fmt.Errorf("expected 2 children for nullable binary array, got %d", len(arr.Children))
	}

	offsetReader, err := NewReader(alloc, arr.Children[0], source)
	if err != nil {
		return nil, fmt.Errorf("creating offset reader: %w", err)
	}

	var validityReader Reader
	if utf8Typ.Nullable {
		validityReader, err = NewReader(alloc, arr.Children[1], source)
		if err != nil {
			return nil, fmt.Errorf("creating validity reader: %w", err)
		}
	}

	return &binaryReader{
		alloc:  alloc,
		arr:    arr,
		source: source,

		offsets:  offsetReader,
		validity: validityReader,
	}, nil
}

func (r *binaryReader) Read(ctx context.Context, alloc *memory.Allocator, count int) (columnar.Array, error) {
	if count <= 0 {
		return nil, fmt.Errorf("count must be positive, got %d", count)
	}

	if !r.initialized {
		// We use the reader's allocator for initializing since the data
		// persists across calls to Read.
		if err := r.init(ctx, r.alloc); err != nil {
			return nil, err
		}
		r.initialized = true
	}

	endOff := min(r.off+count, r.arr.RowCount)
	n := endOff - r.off
	if n == 0 {
		return nil, io.EOF
	}

	// Slice offsets for this batch. The offset array has N+1 values, so we
	// take elements [off, off+n] inclusive to get n+1 offsets for n elements.
	slicedOffsets := r.offsetData[r.off : r.off+n+1]

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

	r.off += n

	// The sliced offsets reference positions within r.data, so we return the
	// entire r.data slice for each call. If we sliced r.data, then we would
	// need to normalize slicedOffsets to be positionally correct.
	return columnar.NewUTF8(r.data, slicedOffsets, validity), nil
}

func (r *binaryReader) init(ctx context.Context, alloc *memory.Allocator) error {
	data, err := r.source.ReadBuffers(ctx, alloc, r.arr.Buffers)
	if err != nil {
		return fmt.Errorf("fetching buffer data: %w", err)
	} else if len(data) != 1 {
		return fmt.Errorf("expected 1 buffer, got %d", len(data))
	}

	// We'll also read all the offsets immediately.
	arr, err := r.offsets.Read(ctx, alloc, math.MaxInt)
	if err != nil {
		return fmt.Errorf("reading offsets: %w", err)
	}
	offsets := arr.(*columnar.Number[int32]).Values()

	r.data = data[0]
	r.offsetData = offsets
	return nil
}

func (r *binaryReader) Reset() {
	r.resetSelf()

	if r.validity != nil {
		r.validity.Reset()
	}
	if r.offsets != nil {
		r.offsets.Reset()
	}
}

func (r *binaryReader) resetSelf() {
	// Reset the offset but keep everything else to allow for re-reading data.
	r.off = 0
}

func (r *binaryReader) Close() error {
	r.resetSelf()

	// Clear state.
	r.initialized = false
	r.data = nil
	r.offsetData = nil

	if r.validity != nil {
		offsetErr := r.offsets.Close()
		validityErr := r.validity.Close()
		return errors.Join(offsetErr, validityErr)
	}
	return r.offsets.Close()
}
