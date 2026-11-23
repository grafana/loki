package dataset

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/memory"
)

func init() {
	// Register the encoding so instances of it can be dynamically created.
	registerValueEncoding(
		datasetmd.PHYSICAL_TYPE_BINARY,
		datasetmd.ENCODING_TYPE_PLAIN,
		registryEntry{
			NewEncoder: func(w streamio.Writer) valueEncoder { return newPlainBytesEncoder(w) },
			NewDecoder: func(data []byte) valueDecoder { return newPlainBytesDecoder(data) },
		},
	)
}

// A plainBytesEncoder encodes byte array values to an [streamio.Writer].
type plainBytesEncoder struct {
	w streamio.Writer
}

var _ valueEncoder = (*plainBytesEncoder)(nil)

// newPlainEncoder creates a plainEncoder that writes encoded strings to w.
func newPlainBytesEncoder(w streamio.Writer) *plainBytesEncoder {
	return &plainBytesEncoder{w: w}
}

// PhysicalType returns [datasetmd.PHYSICAL_TYPE_BINARY].
func (enc *plainBytesEncoder) PhysicalType() datasetmd.PhysicalType {
	return datasetmd.PHYSICAL_TYPE_BINARY
}

// EncodingType returns [datasetmd.ENCODING_TYPE_PLAIN].
func (enc *plainBytesEncoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_PLAIN
}

// Encode encodes an individual string value.
func (enc *plainBytesEncoder) Encode(v Value) error {
	if v.Type() != datasetmd.PHYSICAL_TYPE_BINARY {
		return fmt.Errorf("plain: invalid value type %v", v.Type())
	}
	sv := v.Binary()

	if err := streamio.WriteUvarint(enc.w, uint64(len(sv))); err != nil {
		return err
	}

	n, err := enc.w.Write(sv)
	if n != len(sv) {
		return fmt.Errorf("short write; expected %d bytes, wrote %d", len(sv), n)
	}
	return err
}

// Flush implements [valueEncoder]. It is a no-op for plainEncoder.
func (enc *plainBytesEncoder) Flush() error {
	return nil
}

// Reset implements [valueEncoder]. It resets the encoder to write to w.
func (enc *plainBytesEncoder) Reset(w streamio.Writer) {
	enc.w = w
}

// plainBytesDecoder decodes byte arrays from a byte slice.
type plainBytesDecoder struct {
	data []byte
	off  int // Byte offset in data of the next value to read or skip.
}

var _ valueDecoder = (*plainBytesDecoder)(nil)

// newPlainBytesDecoder creates a decoder that reads encoded strings from data.
func newPlainBytesDecoder(data []byte) *plainBytesDecoder {
	return &plainBytesDecoder{data: data}
}

// PhysicalType returns [datasetmd.PHYSICAL_TYPE_BINARY].
func (dec *plainBytesDecoder) PhysicalType() datasetmd.PhysicalType {
	return datasetmd.PHYSICAL_TYPE_BINARY
}

// EncodingType returns [datasetmd.ENCODING_TYPE_PLAIN].
func (dec *plainBytesDecoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_PLAIN
}

// Decode decodes up to count values using the provided allocator to store the
// values. At the end of the stream, Decode returns nil, [io.EOF].
//
// If the data ends before count values, Decode returns the values that exist
// together with [io.EOF]. If a length prefix is truncated or malformed, or a
// value runs past the end of the data, Decode returns an error and no values.
//
// Decode allocates exactly the memory that the decoded values need. It does not
// size the memory from the whole page.
//
// The return value is a [columnar.UTF8].
func (dec *plainBytesDecoder) Decode(alloc *memory.Allocator, count int) (columnar.Array, error) {
	// Walk the length prefixes first. Without the total size, the values buffer
	// would need the size of the whole page, or it would grow while the loop below
	// fills it.
	n, size, end, err := dec.peekValuesSize(count)
	if err != nil {
		dec.off = end
		return nil, err
	}

	var eof error
	if n < count {
		// peekValuesSize stops early without an error only where the data ends.
		eof = io.EOF
		if n == 0 {
			dec.off = end
			return nil, eof
		}
	}

	var (
		// Offsets are in pairs, so there is always one more offset than values.
		offsetsBuf = memory.NewBuffer[int32](alloc, n+1)
		valuesBuf  = memory.NewBuffer[byte](alloc, size)

		// It is far more efficient to manipulate the output slices ourselves.
		offsets = offsetsBuf.Data()[:n+1]
		values  = valuesBuf.Data()[:size]

		// Store the state on the stack to avoid indirection.
		data = dec.data
		off  = dec.off

		totalBytes int // Last offset to values written.
	)

	// First offset is always 0.
	offsets[0] = 0

	for i := range n {
		// peekValuesSize already checked every length prefix.
		stringSize, uvarintSize := binary.Uvarint(data[off:])
		start := off + uvarintSize
		off = start + int(stringSize)

		copy(values[totalBytes:], data[start:off])
		totalBytes += int(stringSize)
		offsets[i+1] = int32(totalBytes)
	}
	dec.off = off

	return columnar.NewUTF8(values, offsets, memory.Bitmap{}), eof
}

// peekValuesSize walks the length prefixes of up to count values, from the
// current position, and does not move the decoder. It returns the number of
// values that it found, their size in bytes without the length prefixes, and
// the offset in the data where they end.
//
// peekValuesSize finds fewer than count values without an error where the data
// ends after a complete value. If a length prefix is truncated or malformed, or
// a value runs past the end of the data, it returns the error, and the offset
// of that value.
func (dec *plainBytesDecoder) peekValuesSize(count int) (n, size, end int, err error) {
	var (
		data = dec.data
		off  = dec.off
	)
	for n < count {
		stringSize, uvarintSize := binary.Uvarint(data[off:])
		if uvarintSize <= 0 || stringSize > uint64(len(data)-off-uvarintSize) {
			return n, size, off, plainValueError(data, off, stringSize, uvarintSize)
		}

		off += uvarintSize + int(stringSize)
		size += int(stringSize)
		n++
	}
	return n, size, off, nil
}

// Skip implements [valueDecoder]. It returns an error and the number of values
// it skipped before the malformed one if a length prefix is truncated or
// malformed, or if a value runs past the end of the data.
func (dec *plainBytesDecoder) Skip(count int) (int, error) {
	var (
		data = dec.data
		off  = dec.off

		skipped int
	)
	for skipped < count {
		stringSize, uvarintSize := binary.Uvarint(data[off:])
		if uvarintSize <= 0 || stringSize > uint64(len(data)-off-uvarintSize) {
			dec.off = off
			return skipped, plainValueError(data, off, stringSize, uvarintSize)
		}
		off += uvarintSize + int(stringSize)
		skipped++
	}

	dec.off = off
	return skipped, nil
}

// Reset implements [valueDecoder]. It resets the decoder to read from data.
func (dec *plainBytesDecoder) Reset(data []byte) {
	dec.data = data
	dec.off = 0
}

// plainValueError returns the error for the value at off in data, whose length
// prefix is stringSize and takes uvarintSize bytes. It returns nil if the data
// ends at off.
func plainValueError(data []byte, off int, stringSize uint64, uvarintSize int) error {
	switch {
	case uvarintSize == 0 && off == len(data):
		return nil
	case uvarintSize == 0:
		return fmt.Errorf("plain: truncated value length at offset %d", off)
	case uvarintSize < 0:
		return fmt.Errorf("plain: malformed value length at offset %d", off)
	default:
		return fmt.Errorf("plain: value of %d bytes at offset %d runs past the end of the data", stringSize, off)
	}
}
