package array

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/memory"
)

type unsigned interface{ uint32 | uint64 }

type bitpackedWriter[T unsigned] struct {
	alloc     *memory.Allocator
	typ       types.Type
	blockSize int

	// pending holds values for the current incomplete block. Once it reaches
	// blockSize, the block is bitpacked immediately and appended to packed.
	pending     memory.Buffer[T]
	packed      memory.Buffer[byte] // Concatenated bitpacked blocks.
	widths      []uint32            // Per-block bit widths, written to widthWriter at Flush.
	widthWriter Writer
	validity    Writer
	nulls       int
	rows        int
}

func newBitpackedWriter(alloc *memory.Allocator, spec Spec, typ types.Type) (Writer, error) {
	if got, want := spec.Kind(), EncodingKindBitpacked; got != want {
		return nil, fmt.Errorf("expected spec kind %s, got %s", want, got)
	}

	bpSpec := spec.(*SpecBitpacked)

	if bpSpec.BlockSize <= 0 {
		return nil, fmt.Errorf("bitpacked block size must be positive, got %d", bpSpec.BlockSize)
	}
	if bpSpec.BlockSize%8 != 0 {
		return nil, fmt.Errorf("bitpacked block size must be a multiple of 8, got %d", bpSpec.BlockSize)
	}
	if bpSpec.Widths == nil {
		return nil, errors.New("bitpacked spec must have a widths spec")
	}

	switch typ := typ.(type) {
	case *types.Uint32:
		return newBitpackedWriterTyped[uint32](alloc, bpSpec, typ, typ.Nullable)
	case *types.Uint64:
		return newBitpackedWriterTyped[uint64](alloc, bpSpec, typ, typ.Nullable)
	default:
		return nil, fmt.Errorf("unsupported type %s for bitpacked encoding", typ)
	}
}

func newBitpackedWriterTyped[T unsigned](alloc *memory.Allocator, spec *SpecBitpacked, typ types.Type, nullable bool) (*bitpackedWriter[T], error) {
	hasValidity := spec.Validity != nil
	if nullable != hasValidity {
		return nil, fmt.Errorf("expected %s to have validity %t, got %t", typ, nullable, hasValidity)
	}

	// TODO(rfratto): We only need uint8 for widths, but there's no support for
	// uint8 in columnar yet.
	widthWriter, err := NewWriter(alloc, spec.Widths, &types.Uint32{Nullable: false})
	if err != nil {
		return nil, fmt.Errorf("creating widths writer: %w", err)
	}

	var validityWriter Writer
	if hasValidity {
		validityWriter, err = NewWriter(alloc, spec.Validity, &types.Bool{Nullable: false})
		if err != nil {
			return nil, err
		}
	}

	return &bitpackedWriter[T]{
		alloc:     alloc,
		typ:       typ,
		blockSize: spec.BlockSize,

		pending:     memory.NewBuffer[T](alloc, spec.BlockSize),
		packed:      memory.NewBuffer[byte](alloc, 0),
		widthWriter: widthWriter,
		validity:    validityWriter,
	}, nil
}

func (w *bitpackedWriter[T]) Append(arr columnar.Array) error {
	numArr, ok := arr.(*columnar.Number[T])
	if !ok {
		return fmt.Errorf("expected *columnar.Number[%T], got %T", *new(T), arr)
	}

	values := numArr.Values()

	if err := validateNulls(w.validity, numArr, len(values)); err != nil {
		return err
	}
	nulls, err := appendNulls(w.alloc, w.validity, numArr, len(values))
	if err != nil {
		return err
	}
	w.nulls += nulls

	// Feed values into pending, flushing complete blocks as we go.
	for len(values) > 0 {
		space := w.blockSize - w.pending.Len()
		n := min(space, len(values))

		copy(w.pending.Next(n), values[:n])
		values = values[n:]
		w.rows += n

		if w.pending.Len() == w.blockSize {
			w.flushBlock()
		}
	}

	return nil
}

// flushBlock bitpacks the current pending block and appends it to packed.
func (w *bitpackedWriter[T]) flushBlock() {
	block := w.pending.Data()

	var maxVal uint64
	for _, v := range block {
		if uint64(v) > maxVal {
			maxVal = uint64(v)
		}
	}
	width := bits.Len64(maxVal)

	bitpackInto(block, width, &w.packed)
	w.widths = append(w.widths, uint32(width))

	w.pending.Resize(0)
}

func (w *bitpackedWriter[T]) Flush(ctx context.Context, sink buffer.Sink) (Array, error) {
	defer w.reset()

	// Flush any remaining partial block.
	if w.pending.Len() > 0 {
		w.flushBlock()
	}

	// Write all block widths to the widths writer in one batch.
	if err := w.widthWriter.Append(columnar.NewNumber(w.widths, memory.Bitmap{})); err != nil {
		return Array{}, fmt.Errorf("appending block widths: %w", err)
	}

	// Flush children: widths first, then validity.
	var children []Array

	widthsArray, err := w.widthWriter.Flush(ctx, sink)
	if err != nil {
		return Array{}, fmt.Errorf("flushing widths writer: %w", err)
	}
	children = append(children, widthsArray)

	if validity := w.validity; validity != nil {
		validityArray, err := validity.Flush(ctx, sink)
		if err != nil {
			return Array{}, fmt.Errorf("flushing validity writer: %w", err)
		}
		children = append(children, validityArray)
	}

	// Write packed data buffer.
	packedData := w.packed.Serialize()
	bufs, err := sink.WriteBuffers(ctx, []buffer.Data{packedData})
	if err != nil {
		return Array{}, fmt.Errorf("writing bitpacked data to a buffer: %w", err)
	}

	return Array{
		Encoding: &EncodingBitpacked{BlockSize: w.blockSize},
		Type:     w.typ,
		Buffers:  bufs,
		RowCount: w.rows,
		Stats: Stats{
			NullCount: w.nulls,
		},
		Children: children,
	}, nil
}

func (w *bitpackedWriter[T]) reset() {
	w.pending.Resize(0)
	w.packed.Resize(0)
	w.widths = nil
	w.nulls = 0
	w.rows = 0
}

// bitpackInto packs values into dst using the given bit width. Values are
// packed sequentially, LSB-first within bytes.
func bitpackInto[T unsigned](values []T, width int, dst *memory.Buffer[byte]) {
	if width == 0 || len(values) == 0 {
		return
	}

	numBytes := (len(values)*width + 7) / 8
	buf := dst.Next(numBytes)
	clear(buf)

	for i, v := range values {
		startBit := i * width
		for b := range width {
			if uint64(v)&(1<<b) != 0 {
				bitPos := startBit + b
				buf[bitPos/8] |= 1 << (bitPos % 8)
			}
		}
	}
}

type bitpackedReader[T unsigned] struct {
	alloc  *memory.Allocator
	arr    Array
	source buffer.Source

	validity Reader

	initialized bool
	widths      []uint32    // Per-block bit widths.
	packedData  buffer.Data // Raw packed bytes for all blocks.
	blockIndex  int         // Next block to unpack.
	byteOff     int         // Byte offset into packedData for the next block.
	rowOff      int         // Total rows already consumed across all blocks.

	// Current unpacked block and read position within it. blockBuf is reused
	// across calls to nextBlock to avoid repeated allocations.
	blockBuf memory.Buffer[T]
	block    []T
	blockOff int
}

func newBitpackedReader(alloc *memory.Allocator, arr Array, source buffer.Source) (Reader, error) {
	if got, want := arr.Encoding.Kind(), EncodingKindBitpacked; got != want {
		return nil, fmt.Errorf("expected encoding kind %s, got %s", want, got)
	}

	switch typ := arr.Type.(type) {
	case *types.Uint32:
		return newBitpackedReaderTyped[uint32](alloc, arr, source, typ.Nullable)
	case *types.Uint64:
		return newBitpackedReaderTyped[uint64](alloc, arr, source, typ.Nullable)
	default:
		return nil, fmt.Errorf("unsupported type %s for bitpacked encoding", arr.Type)
	}
}

func newBitpackedReaderTyped[T unsigned](alloc *memory.Allocator, arr Array, source buffer.Source, nullable bool) (*bitpackedReader[T], error) {
	enc := arr.Encoding.(*EncodingBitpacked)

	// Determine expected children: widths + optional validity.
	expectChildren := 1
	if nullable {
		expectChildren = 2
	}

	if len(arr.Children) != expectChildren {
		return nil, fmt.Errorf("expected %d children for bitpacked array (nullable=%t), got %d", expectChildren, nullable, len(arr.Children))
	}

	if enc.BlockSize <= 0 {
		return nil, fmt.Errorf("bitpacked block size must be positive, got %d", enc.BlockSize)
	}

	var validityReader Reader
	if nullable {
		var err error
		validityReader, err = NewReader(alloc, arr.Children[1], source)
		if err != nil {
			return nil, fmt.Errorf("creating validity reader: %w", err)
		}
	}

	return &bitpackedReader[T]{
		alloc:  alloc,
		arr:    arr,
		source: source,

		validity: validityReader,
		blockBuf: memory.NewBuffer[T](alloc, enc.BlockSize),
	}, nil
}

func (r *bitpackedReader[T]) Read(ctx context.Context, alloc *memory.Allocator, count int) (columnar.Array, error) {
	if count <= 0 {
		return nil, fmt.Errorf("count must be positive, got %d", count)
	}

	if !r.initialized {
		if err := r.init(ctx, r.alloc); err != nil {
			return nil, err
		}
		r.initialized = true
	}

	// Collect values across block boundaries to satisfy count.
	result := memory.NewBuffer[T](alloc, count)

	for result.Len() < count {
		// If the current block is exhausted, unpack the next one.
		if r.blockOff >= len(r.block) {
			if err := r.nextBlock(); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return nil, err
			}
		}

		n := min(count-result.Len(), len(r.block)-r.blockOff)
		result.Append(r.block[r.blockOff : r.blockOff+n]...)
		r.blockOff += n
	}

	if result.Len() == 0 {
		return nil, io.EOF
	}

	var validity memory.Bitmap
	if r.validity != nil {
		validityArr, err := r.validity.Read(ctx, alloc, result.Len())
		if err != nil {
			return nil, fmt.Errorf("reading validity: %w", err)
		}

		validityBoolArr := validityArr.(*columnar.Bool)
		validity = validityBoolArr.Values()
	}

	return columnar.NewNumber(result.Data(), validity), nil
}

func (r *bitpackedReader[T]) init(ctx context.Context, alloc *memory.Allocator) error {
	// Read widths child array.
	widthsReader, err := NewReader(alloc, r.arr.Children[0], r.source)
	if err != nil {
		return fmt.Errorf("creating widths reader: %w", err)
	}
	defer widthsReader.Close()

	widthsArr, err := widthsReader.Read(ctx, alloc, r.arr.Children[0].RowCount)
	if err != nil {
		return fmt.Errorf("reading widths: %w", err)
	}
	r.widths = widthsArr.(*columnar.Number[uint32]).Values()

	// Read packed data buffer.
	data, err := r.source.ReadBuffers(ctx, alloc, r.arr.Buffers)
	if err != nil {
		return fmt.Errorf("fetching buffer data: %w", err)
	} else if len(data) != 1 {
		return fmt.Errorf("expected 1 buffer, got %d", len(data))
	}
	r.packedData = data[0]

	return nil
}

// nextBlock unpacks the next block into r.block. Returns io.EOF when all
// blocks have been consumed.
func (r *bitpackedReader[T]) nextBlock() error {
	if r.blockIndex >= len(r.widths) {
		return io.EOF
	}

	enc := r.arr.Encoding.(*EncodingBitpacked)
	width := int(r.widths[r.blockIndex])
	blockRows := min(enc.BlockSize, r.arr.RowCount-r.rowOff)

	r.blockBuf.Grow(blockRows)
	r.blockBuf.Resize(blockRows)
	r.blockBuf.Clear()

	// We only need to unpack if width is non-zero; if the width is zero (all
	// zero values in the block), blockBuf is already "unpacked" via the call to
	// Clear above.
	if width != 0 {
		blockBytes := (blockRows*width + 7) / 8
		if r.byteOff+blockBytes > len(r.packedData) {
			return fmt.Errorf("packed data too short: need %d bytes, have %d", r.byteOff+blockBytes, len(r.packedData))
		}

		bitunpackInto(r.packedData[r.byteOff:r.byteOff+blockBytes], r.blockBuf.Data(), width)
		r.byteOff += blockBytes
	}
	r.block = r.blockBuf.Data()

	r.blockOff = 0
	r.blockIndex++
	r.rowOff += blockRows
	return nil
}

// bitunpackInto unpacks values from packed data into dst using the given bit
// width. dst must be pre-sized and zeroed.
func bitunpackInto[T unsigned](data []byte, dst []T, width int) {
	if width == 0 {
		return
	}

	var mask uint64
	if width < 64 {
		mask = (1 << width) - 1
	} else {
		mask = ^uint64(0)
	}

	for i := range dst {
		startBit := i * width
		byteOff := startBit >> 3
		bitOff := uint(startBit & 7)

		// Fast path: read a uint64 covering the packed value.
		if byteOff+8 <= len(data) {
			raw := binary.LittleEndian.Uint64(data[byteOff:])
			v := raw >> bitOff

			// Handle values spanning more than 8 bytes (width+bitOff > 64).
			if remaining := 64 - bitOff; remaining < uint(width) && byteOff+8 < len(data) {
				v |= uint64(data[byteOff+8]) << remaining
			}

			dst[i] = T(v & mask)
			continue
		}

		// Slow path: near the end of data, read remaining bytes.
		var raw uint64
		for j := byteOff; j < len(data); j++ {
			raw |= uint64(data[j]) << uint((j-byteOff)*8)
		}
		dst[i] = T((raw >> bitOff) & mask)
	}
}

func (r *bitpackedReader[T]) Reset() {
	r.resetSelf()

	if r.validity != nil {
		r.validity.Reset()
	}
}

func (r *bitpackedReader[T]) resetSelf() {
	// Reset the offset but keep everything else to allow for re-reading data.
	r.blockIndex = 0
	r.byteOff = 0
	r.rowOff = 0
	r.block = nil
	r.blockOff = 0
}

func (r *bitpackedReader[T]) Close() error {
	r.resetSelf()

	r.initialized = false
	r.packedData = nil
	r.widths = nil

	if r.validity != nil {
		return r.validity.Close()
	}
	return nil
}
