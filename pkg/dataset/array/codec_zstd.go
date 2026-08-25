package array

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/klauspost/compress/zstd"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Shared zstd decoder. GOMAXPROCS concurrency, checksum disabled (the dataset
// layer computes CRC32 on each page).
var zstdDecoder = sync.OnceValues(func() (*zstd.Decoder, error) {
	return zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(0),
		zstd.IgnoreChecksum(true),
	)
})

// Shared zstd encoder. SpeedDefault, CRC disabled.
var zstdEncoder = sync.OnceValues(func() (*zstd.Encoder, error) {
	return zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedDefault),
		zstd.WithEncoderCRC(false),
	)
})

type zstdWriter struct {
	alloc *memory.Allocator
	typ   *types.UTF8

	offsetWriter Writer
	validity     Writer

	initialized bool
	data        memory.Buffer[byte]
	nulls       int
	rows        int
}

func newZstdWriter(alloc *memory.Allocator, spec Spec, typ types.Type) (Writer, error) {
	if got, want := spec.Kind(), EncodingKindZstd; got != want {
		return nil, fmt.Errorf("expected spec kind %s, got %s", want, got)
	} else if got, want := typ.Kind(), types.KindUTF8; got != want {
		return nil, fmt.Errorf("expected type %s, got %s", want, got)
	}

	var (
		zstdSpec = spec.(*SpecZstd)
		utf8Typ  = typ.(*types.UTF8)
	)

	if zstdSpec.Offsets == nil {
		return nil, errors.New("zstd spec requires an offsets spec")
	}

	hasValidity := zstdSpec.Validity != nil
	if utf8Typ.Nullable != hasValidity {
		return nil, fmt.Errorf("expected %s to have validity %t, got %t", utf8Typ, utf8Typ.Nullable, hasValidity)
	}

	offsetWriter, err := NewWriter(alloc, zstdSpec.Offsets, &types.Int32{Nullable: false})
	if err != nil {
		return nil, fmt.Errorf("creating offset writer: %w", err)
	}

	var validityWriter Writer
	if hasValidity {
		validityWriter, err = NewWriter(alloc, zstdSpec.Validity, &types.Bool{Nullable: false})
		if err != nil {
			return nil, err
		}
	}

	return &zstdWriter{
		alloc: alloc,
		typ:   utf8Typ,

		data:         memory.NewBuffer[byte](alloc, 0),
		offsetWriter: offsetWriter,
		validity:     validityWriter,
	}, nil
}

func (w *zstdWriter) Append(arr columnar.Array) error {
	utf8Arr, ok := arr.(*columnar.UTF8)
	if !ok {
		return fmt.Errorf("expected *columnar.UTF8, got %T", arr)
	}

	if !w.initialized {
		w.init()
		w.initialized = true
	}

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

	var (
		baseOffset = int32(w.data.Len())

		srcData    = utf8Arr.Data()
		srcOffsets = utf8Arr.Offsets()
		dataStart  = srcOffsets[0]
		dataEnd    = srcOffsets[len(srcOffsets)-1]
	)

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

func (w *zstdWriter) init() {
	// Write the leading zero offset, since the offset array has N+1 values.
	leading := columnar.NewNumber([]int32{0}, memory.Bitmap{})

	// offsetWriter.Append cannot fail for a single zero value written to a
	// fresh writer, so we intentionally ignore the error.
	_ = w.offsetWriter.Append(leading)
}

func (w *zstdWriter) Flush(ctx context.Context, sink buffer.Sink) (Array, error) {
	defer w.reset()

	enc, err := zstdEncoder()
	if err != nil {
		return Array{}, fmt.Errorf("getting zstd encoder: %w", err)
	}

	// Compress the accumulated data buffer into a pre-allocated buffer and
	// write it to the sink.
	raw := w.data.Serialize()
	encBuf := memory.NewBuffer[byte](w.alloc, len(raw))
	compressed := enc.EncodeAll(raw, encBuf.Data())

	bufs, err := sink.WriteBuffers(ctx, []buffer.Data{compressed})
	if err != nil {
		return Array{}, fmt.Errorf("writing zstd data to a buffer: %w", err)
	}

	// Flush children (offsets, validity).
	var children []Array

	offsetArray, err := w.offsetWriter.Flush(ctx, sink)
	if err != nil {
		return Array{}, fmt.Errorf("flushing offset writer: %w", err)
	}
	children = append(children, offsetArray)

	if w.validity != nil {
		validityArray, err := w.validity.Flush(ctx, sink)
		if err != nil {
			return Array{}, fmt.Errorf("flushing validity writer: %w", err)
		}
		children = append(children, validityArray)
	}

	return Array{
		Encoding: &EncodingZstd{UncompressedSize: len(raw)},
		Type:     w.typ,
		Buffers:  bufs,
		RowCount: w.rows,
		Stats: Stats{
			NullCount: w.nulls,
		},
		Children: children,
	}, nil
}

func (w *zstdWriter) reset() {
	w.initialized = false
	w.data = memory.NewBuffer[byte](w.alloc, 0)
	w.nulls = 0
	w.rows = 0
}

type zstdReader struct {
	alloc  *memory.Allocator
	arr    Array
	source buffer.Source

	offsets  Reader
	validity Reader

	initialized      bool
	data             []byte
	offsetData       []int32
	off              int // Row offset into data
	uncompressedSize int
}

func newZstdReader(alloc *memory.Allocator, arr Array, source buffer.Source) (*zstdReader, error) {
	if got, want := arr.Encoding.Kind(), EncodingKindZstd; got != want {
		return nil, fmt.Errorf("expected encoding kind %s, got %s", want, got)
	} else if got, want := arr.Type.Kind(), types.KindUTF8; got != want {
		return nil, fmt.Errorf("expected type %s, got %s", want, got)
	}

	var (
		utf8Typ = arr.Type.(*types.UTF8)
		enc     = arr.Encoding.(*EncodingZstd)
	)

	switch {
	case !utf8Typ.Nullable && len(arr.Children) != 1:
		return nil, fmt.Errorf("expected 1 child for non-nullable zstd array, got %d", len(arr.Children))
	case utf8Typ.Nullable && len(arr.Children) != 2:
		return nil, fmt.Errorf("expected 2 children for nullable zstd array, got %d", len(arr.Children))
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

	return &zstdReader{
		alloc:  alloc,
		arr:    arr,
		source: source,

		offsets:          offsetReader,
		validity:         validityReader,
		uncompressedSize: enc.UncompressedSize,
	}, nil
}

func (r *zstdReader) Read(ctx context.Context, alloc *memory.Allocator, count int) (columnar.Array, error) {
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

func (r *zstdReader) init(ctx context.Context, alloc *memory.Allocator) error {
	data, err := r.source.ReadBuffers(ctx, alloc, r.arr.Buffers)
	if err != nil {
		return fmt.Errorf("fetching buffer data: %w", err)
	} else if len(data) != 1 {
		return fmt.Errorf("expected 1 buffer, got %d", len(data))
	}

	// Decompress the data buffer into a pre-allocated, allocator-managed buffer.
	dec, err := zstdDecoder()
	if err != nil {
		return fmt.Errorf("getting zstd decoder: %w", err)
	}

	decBuf := memory.NewBuffer[byte](alloc, r.uncompressedSize)
	decompressed, err := dec.DecodeAll(data[0], decBuf.Data())
	if err != nil {
		return fmt.Errorf("decompressing zstd data: %w", err)
	}

	// Read all offsets.
	arr, err := r.offsets.Read(ctx, alloc, math.MaxInt)
	if err != nil {
		return fmt.Errorf("reading offsets: %w", err)
	}
	offsets := arr.(*columnar.Number[int32]).Values()

	r.data = decompressed
	r.offsetData = offsets
	return nil
}

func (r *zstdReader) Reset() {
	r.resetSelf()

	if r.validity != nil {
		r.validity.Reset()
	}
	if r.offsets != nil {
		r.offsets.Reset()
	}
}

func (r *zstdReader) resetSelf() {
	// Reset the offset but keep everything else to allow for re-reading data.
	r.off = 0
}

func (r *zstdReader) Close() error {
	r.resetSelf()

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
