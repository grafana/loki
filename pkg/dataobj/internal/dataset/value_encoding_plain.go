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
	var (
		// Strings need a an offsets and a value buffer.
		//
		// Offsets are in pairs, so there's always one additional offset from the
		// requested count.
		//
		// Meanwhile, there's no good way of knowing how many bytes we might need to
		// store all the strings. It's probably better to overestimate so we have
		// exactly one allocated reusable memory region than to have it grow a few
		// times as we try to discover the true size.

		offsetsBuf = memory.NewBuffer[int32](alloc, count+1)
		valuesBuf  = memory.NewBuffer[byte](alloc, len(dec.data))

		// It's going to be far more efficient for us to manipulate the output
		// slices ourselves, so we'll do that here.

		offsets = offsetsBuf.Data()[:count+1]
		values  = valuesBuf.Data()[:len(dec.data)]

		totalBytes int // Last offset to values written.
	)

	// Store state on stack to avoid indirection.
	var (
		data = dec.data
		off  = dec.off
	)
	defer func() { dec.off = off }()

	// First offset is always 0.
	offsets[0] = 0

	for i := range count {
		stringSize, uvarintSize := binary.Uvarint(data[off:])
		if uvarintSize <= 0 {
			if i == 0 {
				return nil, io.EOF
			}

			return columnar.NewUTF8(
				values[:totalBytes],
				offsets[:i+1],
				memory.Bitmap{},
			), io.EOF
		}

		copied := copy(values[totalBytes:], data[off+uvarintSize:off+uvarintSize+int(stringSize)])

		off += uvarintSize + copied
		totalBytes += int(stringSize)
		offsets[i+1] = int32(totalBytes)
	}

	return columnar.NewUTF8(
		values[:totalBytes],
		offsets[:count+1],
		memory.Bitmap{},
	), nil
}

// Reset implements [valueDecoder]. It resets the decoder to read from data.
func (dec *plainBytesDecoder) Reset(data []byte) {
	dec.data = data
	dec.off = 0
}
