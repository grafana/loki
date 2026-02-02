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
		datasetmd.PHYSICAL_TYPE_INT64,
		datasetmd.ENCODING_TYPE_DELTA,
		registryEntry{
			NewEncoder: func(w streamio.Writer) valueEncoder { return newDeltaEncoder(w) },
			NewDecoder: func(data []byte) valueDecoder { return newDeltaDecoder(data) },
		},
	)
}

// deltaEncoder encodes delta-encoded int64s. Values are encoded as varint,
// with each subsequent value being the delta from the previous value.
type deltaEncoder struct {
	w    streamio.Writer
	prev int64
}

var _ valueEncoder = (*deltaEncoder)(nil)

// newDeltaEncoder creates a deltaEncoder that writes encoded numbers to w.
func newDeltaEncoder(w streamio.Writer) *deltaEncoder {
	var enc deltaEncoder
	enc.Reset(w)
	return &enc
}

// PhysicalType returns [datasetmd.PHYSICAL_TYPE_INT64].
func (enc *deltaEncoder) PhysicalType() datasetmd.PhysicalType {
	return datasetmd.PHYSICAL_TYPE_INT64
}

// EncodingType returns [datasetmd.ENCODING_TYPE_DELTA].
func (enc *deltaEncoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_DELTA
}

// Encode encodes a new value.
func (enc *deltaEncoder) Encode(v Value) error {
	if v.Type() != datasetmd.PHYSICAL_TYPE_INT64 {
		return fmt.Errorf("delta: invalid value type %v", v.Type())
	}
	iv := v.Int64()

	delta := iv - enc.prev
	enc.prev = iv
	return streamio.WriteVarint(enc.w, delta)
}

// Flush implements [valueEncoder]. It is a no-op for deltaEncoder.
func (enc *deltaEncoder) Flush() error {
	return nil
}

// Reset resets the encoder to its initial state.
func (enc *deltaEncoder) Reset(w streamio.Writer) {
	enc.prev = 0
	enc.w = w
}

// deltaDecoder decodes delta-encoded numbers. Values are decoded as varint,
// with each subsequent value being the delta from the previous value.
type deltaDecoder struct {
	buf  []byte
	off  int
	prev int64
}

var _ valueDecoder = (*deltaDecoder)(nil)

// newDeltaDecoder creates a deltaDecoder that reads encoded numbers from data.
func newDeltaDecoder(data []byte) *deltaDecoder {
	var dec deltaDecoder
	dec.Reset(data)
	return &dec
}

// PhysicalType returns [datasetmd.PHYSICAL_TYPE_INT64].
func (dec *deltaDecoder) PhysicalType() datasetmd.PhysicalType {
	return datasetmd.PHYSICAL_TYPE_INT64
}

// Type returns [datasetmd.ENCODING_TYPE_DELTA].
func (dec *deltaDecoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_DELTA
}

// Decode decodes up to count values, storing the results into a new
// [columnar.Int64] array obtained from the provided allocator. At the end of
// the stream, Decode returns an [io.EOF].
func (dec *deltaDecoder) Decode(alloc *memory.Allocator, count int) (columnar.Array, error) {
	// Obtain a buffer from the allocator with enough capacity for an optimistic `count` values.
	// Resize the buffer explicitly in order to use the Set API which avoids a reslice compared to Push.
	// Resize must be used again before returning any data if the slice is not completely filled.
	valuesBuf := memory.NewBuffer[int64](alloc, count)
	valuesBuf.Resize(count)
	values := valuesBuf.Data()

	// Shadow local variables to avoid the pointer indirection of referencing dec.buf and dec.prev.
	var (
		buf  []byte
		prev int64
		off  int
	)
	buf = dec.buf
	prev = dec.prev
	off = dec.off
	defer func() { dec.buf = buf; dec.prev = prev; dec.off = off }()

	// Check the invariant so the compiler can eliminate the bounds check when assigning to values[i].
	if len(values) != count {
		panic(fmt.Sprintf("invariant broken: values buffer has %d values, expected %d", len(values), count))
	}

	for i := range count {
		delta, n := binary.Varint(buf[off:])
		if n <= 0 {
			valuesBuf.Resize(i)
			return columnar.NewNumber[int64](values[:i], memory.Bitmap{}), io.EOF
		}

		off += n
		prev += delta
		values[i] = prev
	}
	return columnar.NewNumber[int64](values, memory.Bitmap{}), nil
}

// Reset resets the deltaDecoder to its initial state.
func (dec *deltaDecoder) Reset(data []byte) {
	dec.prev = 0
	dec.off = 0
	dec.buf = data
}
