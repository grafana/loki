package dataset

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

func init() {
	// Register the encoding so instances of it can be dynamically created.
	registerValueEncoding(
		datasetmd.VALUE_TYPE_INT64,
		datasetmd.ENCODING_TYPE_DELTA,
		func(w streamio.Writer) valueEncoder { return newDeltaEncoder(w) },
		func(r streamio.Reader) valueDecoder { return newDeltaDecoder(r) },
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

// ValueType returns [datasetmd.VALUE_TYPE_INT64].
func (enc *deltaEncoder) ValueType() datasetmd.ValueType {
	return datasetmd.VALUE_TYPE_INT64
}

// EncodingType returns [datasetmd.ENCODING_TYPE_DELTA].
func (enc *deltaEncoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_DELTA
}

// Encode encodes a new value.
func (enc *deltaEncoder) Encode(v Value) error {
	if v.Type() != datasetmd.VALUE_TYPE_INT64 {
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
	r    streamio.Reader
	prev int64
}

var _ valueDecoder = (*deltaDecoder)(nil)

// newDeltaDecoder creates a deltaDecoder that reads encoded numbers from r.
func newDeltaDecoder(r streamio.Reader) *deltaDecoder {
	var dec deltaDecoder
	dec.Reset(r)
	return &dec
}

// ValueType returns [datasetmd.VALUE_TYPE_INT64].
func (dec *deltaDecoder) ValueType() datasetmd.ValueType {
	return datasetmd.VALUE_TYPE_INT64
}

// Type returns [datasetmd.ENCODING_TYPE_DELTA].
func (dec *deltaDecoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_DELTA
}

// Decode decodes the next value.
func (dec *deltaDecoder) Decode() (Value, error) {
	delta, err := streamio.ReadVarint(dec.r)
	if err != nil {
		return Int64Value(dec.prev), err
	}

	dec.prev += delta
	return Int64Value(dec.prev), nil
}

// Reset resets the deltaDecoder to its initial state.
func (dec *deltaDecoder) Reset(r streamio.Reader) {
	dec.prev = 0
	dec.r = r
}
