package dataset

import (
	"fmt"
	"io"
	"math/bits"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

func init() {
	// Register the encoding so instances of it can be dynamically created.
	registerValueEncoding(
		datasetmd.VALUE_TYPE_UINT64,
		datasetmd.ENCODING_TYPE_BITMAP,
		func(w streamio.Writer) valueEncoder { return newBitmapEncoder(w) },
		func(r streamio.Reader) valueDecoder { return newBitmapDecoder(r) },
	)
}

const maxRunLength uint64 = 1<<63 - 1 // 2^63-1

// bitmapEncoder encodes and decodes bitmaps of unsigned numbers up to 64 bits
// wide. To use bitmap with signed integers, callers should first encode the
// integers using zig-zag encoding to minimize the number of bits needed for
// negative values.
//
// Data is encoded with a hybrid of run-length encoding and bitpacking. Longer
// sequences of the same value are encoded with run-length encoding, while
// shorter sequences are bitpacked when possible. To avoid padding, bitpacking
// is only used when there are a multiple of 8 values to encode.
//
// Bitpacking is done using a dynamic bit width. The bit width is determined by
// the largest value in the set of 8 values. The bit width can be any value
// from 1 to 64, inclusive.
//
// # Format
//
// The bitmap format is a slight modification of the Parquet format to support
// longer runs and support streaming. The EBNF grammar is as follows:
//
//	bitmap             = run+;
//	run                = bit_packed_run | rle_run;
//	bit_packed_run     = bit_packed_header bit_packed_values;
//	bit_packed_header  = (* uvarint(bit_packed_sets << 7 | bit_width << 1 | 1) *)
//	bit_packed_sets    = (* value between 1 and 2^57-1, inclusive; each set has 8 elements *)
//	bit_width          = (* bit size of element in set; value between 1 and 64, inclusive *)
//	bit_packed_values  = (* least significant bit of each byte to most significant bit of each byte *)
//	rle_run            = rle_header repeated_value;
//	rle_header         = (* uvarint(rle_run_len << 1) *)
//	rle_run_len        = (* value between 1 and 2^63-1, inclusive *)
//	repeated_value     = (* repeated value encoded as uvarint *)
//
// Where this differs from Parquet:
//
//   - We don't use a fixed width for bitpacked values, to allow for streaming
//     calls to Encode without knowing the width in advance. Instead, the width
//     is determined when flushing a bitpacked set to an internal buffer.
//
//     To minimize the overhead of encoding the width dynamically, we store
//     each bitpacked set with the smallest amount of bits possible. If two
//     sets have different widths, they are flushed as two different runs.
//
//   - For simplicity, repeated_value is encoded as uvarint rather than
//     flushing the value in its entirety.
//
//   - To facilitate streaming, we don't prepend the length of all bytes
//     written. Callers may choose to prepend the length. Without the length,
//     readers must take caution to not read past the end of the RLE sequence
//     by knowing exactly how many values were encoded.
type bitmapEncoder struct {
	w streamio.Writer

	// bitmapEncoder is a basic state machine with three states:
	//
	// READY    The default state; no values are being tracked. Appending a new
	//          value moves to the RLE state.
	//
	// RLE      bitmapEncoder is tracking a run of runValue. Active when
	//          runLength>0. If a value not matching runValue is appended and
	//          runLength<8, the state moves to BITPACK. Otherwise, the previous
	//          run is flushed and a new RLE run is started.
	//
	// BITPACK  bitmapEncoder is tracking a set of values to bitpack. Active when
	//          setSize>0. Once the set reaches 8 values, the set is flushed and
	//          the encoder resets to READY.

	runValue  uint64 // Value in the current run.
	runLength uint64 // Length of the current run.

	set     [8]uint64 // Set of values to bitpack.
	setSize byte      // Current number of elements in set.

	buf *bitpackBuffer // Buffer for multiple runs of bitpacked sets of the same bit width.
}

// newBitmapEncoder creates a new bitmap encoder that writes encoded numbers to w.
func newBitmapEncoder(w streamio.Writer) *bitmapEncoder {
	return &bitmapEncoder{
		w:   w,
		buf: newBitpackBuffer(),
	}
}

// ValueType returns [datasetmd.VALUE_TYPE_UINT64].
func (enc *bitmapEncoder) ValueType() datasetmd.ValueType {
	return datasetmd.VALUE_TYPE_UINT64
}

// EncodingType returns [datasetmd.ENCODING_TYPE_BITMAP].
func (enc *bitmapEncoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_BITMAP
}

// Encode appends a new uint64 value to enc.
//
// When flushing, Encode returns an error if writing to the underlying
// [streamio.Writer] fails.
//
// Call [bitmapEncoder.Flush] to end the current run and flush any remaining
// values.
func (enc *bitmapEncoder) Encode(v Value) error {
	if v.Type() != datasetmd.VALUE_TYPE_UINT64 {
		return fmt.Errorf("invalid value type %s", v.Type())
	}
	uv := v.Uint64()

	switch {
	case enc.runLength == 0 && enc.setSize == 0: // READY; start a new run.
		enc.runValue = uv
		enc.runLength = 1
		return nil

	case enc.runLength > 0 && uv == enc.runValue: // RLE with matching value; continue the run.
		enc.runLength++

		// If we hit the maximum run length, flush immediately.
		if enc.runLength == maxRunLength {
			return enc.flushRLE()
		}
		return nil

	case enc.runLength > 0 && uv != enc.runValue: // RLE with different value; the run ended.
		// If the run lasted less than 8 values, we switch to the BITPACK state.
		if enc.runLength < 8 {
			// Copy over the existing run to the set and then add the new value.
			enc.setSize = byte(enc.runLength)
			for i := byte(0); i < enc.setSize; i++ {
				enc.set[i] = enc.runValue
			}

			enc.runLength = 0
			enc.set[enc.setSize] = uv
			enc.setSize++

			if enc.setSize == 8 {
				return enc.flushBitpacked()
			}
			return nil
		}

		// Otherwise, flush the previous run and start a new one.
		if err := enc.flushRLE(); err != nil {
			return err
		}

		enc.runValue = uv
		enc.runLength = 1
		return nil

	case enc.setSize > 0: // BITPACK; add the value to the set.
		enc.set[enc.setSize] = uv
		enc.setSize++

		if enc.setSize == 8 {
			return enc.flushBitpacked()
		}
		return nil

	default:
		panic("dataset.bitmapEncoder: invalid state")
	}
}

// Flush writes any remaining values to the underlying [streamio.Writer].
func (enc *bitmapEncoder) Flush() error {
	// We always flush using RLE. If bitmapEncoder was in the BITPACK state, we
	// don't have 8 values yet; if we did, they would've already been flushed in
	// Encode.
	return enc.flushRLE()
}

func (enc *bitmapEncoder) flushRLE() error {
	// Flush anything in the bitpack buffer.
	if err := enc.buf.Flush(enc.w); err != nil {
		return err
	}

	switch {
	case enc.runLength > 0:
		if enc.runLength > maxRunLength {
			return fmt.Errorf("run length too large")
		}

		// Header.
		if err := streamio.WriteUvarint(enc.w, enc.runLength<<1); err != nil {
			return err
		}

		// Value.
		if err := streamio.WriteUvarint(enc.w, enc.runValue); err != nil {
			return err
		}

		enc.runLength = 0
		enc.runValue = 0
		return nil

	case enc.setSize > 0:
		// Cosnume the set as a sequence of runs.
		for off := byte(0); off < enc.setSize; {
			var (
				val = enc.set[off]
				run = byte(1)
			)

			for j := off + 1; j < enc.setSize; j++ {
				if enc.set[j] != val {
					break
				}
				run++
			}

			off += run

			// Header.
			if err := streamio.WriteUvarint(enc.w, uint64(run<<1)); err != nil {
				return err
			}

			// Value.
			if err := streamio.WriteUvarint(enc.w, val); err != nil {
				return err
			}
		}

		enc.setSize = 0
		return nil

	default:
		return nil
	}
}

// flushBitpacked flushes the current bitpacked buffer for accumulating runs.
// If the buffer is full, we flush the buffer immediately to the underlying
// writer.
func (enc *bitmapEncoder) flushBitpacked() error {
	if enc.setSize != 8 {
		panic("dataset.bitmapEncoder: flushBitpacked called with less than 8 values")
	}

	// Detect the bit width of the set.
	width := 1
	for i := 0; i < int(enc.setSize); i++ {
		if bitLength := bits.Len64(enc.set[i]); bitLength > width {
			width = bitLength
		}
	}

	// Write out the bitpacked values. Bitpacking 8 values of bit width N always
	// requires exactly N bytes.
	//
	// Each value is packed from the least significant bit of each byte to the
	// most significant bit, while still retaining the order of bits from the
	// original value.
	//
	// This bitpacking algorithm is challenging to reason about, but is retained
	// to align with Parquet's behaviour. I (rfratto) found it easier to reason
	// by considering how the output bits map to the input bits.
	//
	// This means that for width == 3:
	//
	//   index:     0   1   2   3   4   5   6   7
	//   dec value: 0   1   2   3   4   5   6   7
	//   bit value: 000 001 010 011 100 101 110 111
	//   bit label: ABC DEF GHI JKL MNO PQR STU VWX
	//
	//   index:     22111000 54443332 77766655
	//   bit value: 10001000 11000110 11111010
	//   bit label: HIDEFABC RMNOJKLG VWXSTUPQ
	//
	// Formatting it as a table better demonstrates the mapping:
	//
	//   Index Labels Input bit  Output byte   Output bit (for byte)
	//   ----- -----  ---------  -----------   ---------------------
	//   0     ABC     2  1  0   0 0 0         2 1 0
	//   1     DEF     5  4  3   0 0 0         5 4 3
	//   2     GHI     8  7  6   1 0 0         0 7 6
	//   3     JKL    11 10  9   1 1 1         3 2 1
	//   4     MNO    14 13 12   1 1 1         6 5 4
	//   5     PQR    17 16 15   2 2 1         1 0 7
	//   6     STU    20 19 18   2 2 2         4 3 2
	//   7     VWX    23 22 21   2 2 2         7 6 5
	//
	// So, for any given output bit, its value originates from:
	//
	//   * enc.set index:       output_bit/width
	//   * enc.set element bit: output_bit%width
	//
	// If there's a much simpler way to understand and do this packing, I'd love
	// to know.
	buf := make([]byte, 0, width)

	for outputByte := 0; outputByte < width; outputByte++ {
		var b byte

		for i := 0; i < 8; i++ {
			outputBit := outputByte*8 + i
			inputIndex := outputBit / width
			inputBit := outputBit % width

			// Set the bit in b.
			if enc.set[inputIndex]&(1<<inputBit) != 0 {
				b |= 1 << i
			}
		}

		buf = append(buf, b)
	}

	// Append the set to our buffer. It only returns an error in two cases:
	//
	// 1. The width changed, or
	// 2. the buffer is full.
	//
	// In either case, we want to flush and try again.
	for range 2 {
		if err := enc.buf.AppendSet(width, buf); err == nil {
			break
		}

		if err := enc.buf.Flush(enc.w); err != nil {
			return err
		}
	}

	enc.setSize = 0
	return nil
}

// Reset resets enc to write to w.
func (enc *bitmapEncoder) Reset(w streamio.Writer) {
	enc.w = w
	enc.runValue = 0
	enc.runLength = 0
	enc.setSize = 0
	enc.buf.Reset()
}

type bitpackBuffer struct {
	maxBufferSize int

	width int

	sets int    // Number of encoded sets. Each set has 8 elements.
	data []byte // Total amount of data.
}

func newBitpackBuffer() *bitpackBuffer {
	return &bitpackBuffer{
		maxBufferSize: 4096, // 4KiB
	}
}

// AppendSet appends a bitpacked set of 8 elements to the buffer. AppendSet
// fails if the buffer is too large or if the width changed.
func (b *bitpackBuffer) AppendSet(width int, data []byte) error {
	if width < 1 || width > 64 {
		return fmt.Errorf("invalid width: %d", width)
	}

	// Error conditions
	switch {
	case len(b.data)+len(data) > b.maxBufferSize && b.sets > 0:
		return fmt.Errorf("buffer full")
	case b.width != width && b.sets > 0:
		return fmt.Errorf("width changed")
	}

	b.sets++
	b.width = width
	b.data = append(b.data, data...)
	return nil
}

// Flush flushes buffered data to w and resets state for more writes.
func (b *bitpackBuffer) Flush(w streamio.Writer) error {
	if b.sets == 0 {
		return nil
	}

	// The header of the bitpacked sequence encodes:
	//
	// * The number of sets
	// * The width of elements in the sets (1-64)
	// * A flag indicating the header type
	//
	// To encode the width in 6 bits, we encode width-1. That reserves the bottom
	// 7 bits for metadata, and the remaining 57 bits for the number of sets.
	const maxSets = 1<<57 - 1

	// Validate constraints for safety.
	switch {
	case b.width < 1 || b.width > 64:
		return fmt.Errorf("invalid width: %d", b.width)
	case b.sets > maxSets:
		// This shouldn't ever happen; 2^57-1 sets, in the best case (width of 1),
		// would require our buffer to be 144PB.
		return fmt.Errorf("too many sets: %d", b.sets)
	}

	// Width can be between 1 and 64. To pack it into 6 bits, we subtract 1 from
	// the value.
	header := (uint64(b.sets) << 7) | (uint64(b.width-1) << 1) | 1
	if err := streamio.WriteUvarint(w, header); err != nil {
		return err
	}

	if n, err := w.Write(b.data); err != nil {
		return err
	} else if n != len(b.data) {
		return fmt.Errorf("short write: %d != %d", n, len(b.data))
	}

	b.width = 0
	b.sets = 0
	b.data = b.data[:0]
	return nil
}

// Reset resets the buffer to its initial state without flushing.
func (b *bitpackBuffer) Reset() {
	b.width = 0
	b.sets = 0
	b.data = b.data[:0]
}

// bitmapDecoder decoes uint64s from a bitmap-encoded stream. See the doc
// comment on [bitmapEncoder] for detail on the format.
type bitmapDecoder struct {
	r streamio.Reader

	// Like [bitmapEncoder], bitmapDecoder is a basic state machine with four
	// states:
	//
	// READY          The default state; the decoder needs to pull a new run
	//                header and move to RLE or BITPACK-READY.
	//
	// RLE            The decoder is in the middle of an RLE-encoded run. Active
	//                when runLength>0. runLength should decrease by 1 each time
	//                Decode is called, returning runValue.
	//
	// BITPACK-READY  The decoder is ready to read a new bitpacked set. Active
	//                when sets>0 and setSize==0. The decoder needs to pull the
	//                next set, update setSize and decrement sets by 1.
	//
	// BITPACK-SET    The decoder is in the middle of a bitpacked set. Active
	//                when setSize>0. setSize decreases by 1 each time Decode is
	//                called, and the next bitpacked value in the set is
	//                returned.
	//
	// bitmapDecoder always starts in the READY state, and the header it pulls
	// determines whether it moves to RLE or BITPACK-READY. After fully consuming
	// a run, it reverts back to READY.

	runValue  uint64 // Value of the current RLE run.
	runLength uint64 // Remaining values in the current RLE run.

	sets     int    // Number of bitpacked sets left to read, each of which contains 8 elements.
	setWidth int    // Number of bits to use for each value. Must be no greater than 64.
	setSize  byte   // Number of values left in the current bitpacked set.
	set      []byte // Current set of bitpacked values.
}

// newBitmapDecoder creates a new bitmap decoder that reads encoded numbers
// from r.
func newBitmapDecoder(r streamio.Reader) *bitmapDecoder {
	return &bitmapDecoder{r: r}
}

// ValueType returns [datasetmd.VALUE_TYPE_UINT64].
func (dec *bitmapDecoder) ValueType() datasetmd.ValueType {
	return datasetmd.VALUE_TYPE_UINT64
}

// EncodingType returns [datasetmd.ENCODING_TYPE_BITMAP].
func (dec *bitmapDecoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_BITMAP
}

// Decode reads the next uint64 value from the stream.
func (dec *bitmapDecoder) Decode() (Value, error) {
	// See comment inside [bitmapDecoder] for the state machine details.

NextState:
	switch {
	case dec.runLength == 0 && dec.sets == 0 && dec.setSize == 0: // READY
		if err := dec.readHeader(); err != nil {
			return Uint64Value(0), fmt.Errorf("reading header: %w", err)
		}
		goto NextState

	case dec.runLength > 0: // RLE
		dec.runLength--
		return Uint64Value(dec.runValue), nil

	case dec.sets > 0 && dec.setSize == 0: // BITPACK-READY
		if err := dec.nextBitpackSet(); err != nil {
			return Uint64Value(0), fmt.Errorf("reading bitpacked set: %w", err)
		}
		goto NextState

	case dec.setSize > 0: // BITPACK-SET
		elem := 8 - dec.setSize

		var val uint64
		for b := 0; b < dec.setWidth; b++ {
			// Read bit b of element index i, where i is byte i*8/width.
			i := (int(elem)*dec.setWidth + b) / 8
			offset := (int(elem)*dec.setWidth + b) % 8
			bitValue := dec.set[i] & (1 << offset) >> offset

			val |= uint64(bitValue) << b
		}

		dec.setSize--
		return Uint64Value(val), nil

	default:
		panic("dataset.bitmapDecoder: invalid state")
	}
}

// readHeader reads the next header from the stream
func (dec *bitmapDecoder) readHeader() error {
	// Ready the next uvarint.
	header, err := streamio.ReadUvarint(dec.r)
	if err != nil {
		return err
	}

	if header&1 == 1 {
		// Start of a bitpacked set.
		dec.sets = int(header >> 7)
		dec.setWidth = int((header>>1)&0x3f) + 1
		dec.setSize = 0 // Sets will be loaded in [bitmapDecoder.nextBitpackSet].
		dec.set = make([]byte, dec.setWidth)
	} else {
		// RLE run.
		runLength := header >> 1

		val, err := streamio.ReadUvarint(dec.r)
		if err != nil {
			return err
		}

		dec.runLength = runLength
		dec.runValue = val
	}

	return nil
}

// nextBitpackSet loads the next bitpack set and decrements the sets counter.
func (dec *bitmapDecoder) nextBitpackSet() error {
	if dec.sets == 0 {
		return fmt.Errorf("no bitpacked sets left")
	}

	// dec.set is allocated in [bitmapDecoder.readHeader].
	if _, err := io.ReadFull(dec.r, dec.set); err != nil {
		return err
	}

	dec.setSize = 8 // Always 8 elements in each set.
	dec.sets--
	return nil
}

// Reset resets dec to read from r.
func (dec *bitmapDecoder) Reset(r streamio.Reader) {
	dec.r = r
	dec.runValue = 0
	dec.runLength = 0
	dec.sets = 0
	dec.setWidth = 0
	dec.setSize = 0
	dec.set = nil
}
