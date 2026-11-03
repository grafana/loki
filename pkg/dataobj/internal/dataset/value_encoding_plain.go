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
	off  int // Last read offset into data.
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
// At the end of the stream, Decode returns nil, [io.EOF].
//
// The return value is a [columnar.UTF8].
func (dec *plainBytesDecoder) Decode(alloc *memory.Allocator, count int) (columnar.Array, error) {
	// A first pass over the length prefixes finds how many bytes the values need.
	// It skips the value bytes, so it costs far less than the copy below. It lets
	// the values buffer have the exact size, instead of the size of the whole
	// page, which a window of a few rows would then waste.
	var (
		found      int
		totalBytes int
	)
	for off := dec.off; found < count; found++ {
		size, end, err := nextPlainValue(dec.data, off)
		if err != nil {
			return nil, err
		} else if end == off {
			break
		}
		off = end
		totalBytes += size
	}
	if found == 0 {
		return nil, io.EOF
	}

	var (
		// Offsets are in pairs, so there's always one additional offset from the
		// found count.
		offsetsBuf = memory.NewBuffer[int32](alloc, found+1)
		valuesBuf  = memory.NewBuffer[byte](alloc, totalBytes)

		// It's going to be far more efficient for us to manipulate the output
		// slices ourselves, so we'll do that here.
		offsets = offsetsBuf.Data()[:found+1]
		values  = valuesBuf.Data()[:totalBytes]

		// Store state on stack to avoid indirection.
		data = dec.data
		off  = dec.off

		written int // Last offset to values written.
	)
	defer func() { dec.off = off }()

	// First offset is always 0.
	offsets[0] = 0

	for i := range found {
		stringSize, uvarintSize := binary.Uvarint(data[off:])
		copied := copy(values[written:], data[off+uvarintSize:off+uvarintSize+int(stringSize)])

		off += uvarintSize + copied
		written += int(stringSize)
		offsets[i+1] = int32(written)
	}

	// The data ended before count values. The caller sees the values that exist
	// together with io.EOF.
	var err error
	if found < count {
		err = io.EOF
	}
	return columnar.NewUTF8(values[:written], offsets[:found+1], memory.Bitmap{}), err
}

// Skip advances past up to count values without copying them. It returns the
// number of values skipped, which is lower than count only if the data ends
// first.
func (dec *plainBytesDecoder) Skip(count int) (int, error) {
	var skipped int
	for ; skipped < count; skipped++ {
		_, end, err := nextPlainValue(dec.data, dec.off)
		if err != nil {
			return skipped, err
		} else if end == dec.off {
			break
		}
		dec.off = end
	}
	return skipped, nil
}

// nextPlainValue reads the length prefix of the value at offset off of data. It
// returns the length of the value and the offset after the value. The offset
// after the value equals off when the data ends at off.
//
// It returns an error if the prefix is malformed or if the value runs past the
// end of data.
func nextPlainValue(data []byte, off int) (size, end int, err error) {
	valueSize, prefixSize := binary.Uvarint(data[off:])
	switch {
	case prefixSize == 0 && off == len(data):
		return 0, off, nil
	case prefixSize == 0:
		return 0, off, fmt.Errorf("plain: truncated value length at offset %d", off)
	case prefixSize < 0:
		return 0, off, fmt.Errorf("plain: malformed value length at offset %d", off)
	case valueSize > uint64(len(data)-off-prefixSize):
		// Comparing as unsigned keeps a huge length from wrapping to a negative int.
		return 0, off, fmt.Errorf("plain: value of %d bytes overruns the page", valueSize)
	}
	return int(valueSize), off + prefixSize + int(valueSize), nil
}

// Reset implements [valueDecoder]. It resets the decoder to read from data.
func (dec *plainBytesDecoder) Reset(data []byte) {
	dec.data = data
	dec.off = 0
}
