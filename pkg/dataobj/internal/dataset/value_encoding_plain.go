package dataset

import (
	"encoding/binary"
	"fmt"
	"io"
	"unsafe"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

func init() {
	// Register the encoding so instances of it can be dynamically created.
	registerValueEncoding(
		datasetmd.VALUE_TYPE_STRING,
		datasetmd.ENCODING_TYPE_PLAIN,
		func(w streamio.Writer) valueEncoder { return newPlainEncoder(w) },
		func(r streamio.Reader) valueDecoder { return newPlainDecoder(r) },
	)
}

// A plainEncoder encodes string values to an [streamio.Writer].
type plainEncoder struct {
	w streamio.Writer
}

var _ valueEncoder = (*plainEncoder)(nil)

// newPlainEncoder creates a plainEncoder that writes encoded strings to w.
func newPlainEncoder(w streamio.Writer) *plainEncoder {
	return &plainEncoder{w: w}
}

// ValueType returns [datasetmd.VALUE_TYPE_STRING].
func (enc *plainEncoder) ValueType() datasetmd.ValueType {
	return datasetmd.VALUE_TYPE_STRING
}

// EncodingType returns [datasetmd.ENCODING_TYPE_PLAIN].
func (enc *plainEncoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_PLAIN
}

// Encode encodes an individual string value.
func (enc *plainEncoder) Encode(v Value) error {
	if v.Type() != datasetmd.VALUE_TYPE_STRING {
		return fmt.Errorf("plain: invalid value type %v", v.Type())
	}
	sv := v.String()

	if err := streamio.WriteUvarint(enc.w, uint64(len(sv))); err != nil {
		return err
	}

	// This saves a few allocations by avoiding a copy of the string.
	// Implementations of io.Writer are not supposed to modifiy the slice passed
	// to Write, so this is generally safe.
	n, err := enc.w.Write(unsafe.Slice(unsafe.StringData(sv), len(sv)))
	if n != len(sv) {
		return fmt.Errorf("short write; expected %d bytes, wrote %d", len(sv), n)
	}
	return err
}

// Flush implements [valueEncoder]. It is a no-op for plainEncoder.
func (enc *plainEncoder) Flush() error {
	return nil
}

// Reset implements [valueEncoder]. It resets the encoder to write to w.
func (enc *plainEncoder) Reset(w streamio.Writer) {
	enc.w = w
}

// plainDecoder decodes strings from an [streamio.Reader].
type plainDecoder struct {
	r streamio.Reader
}

var _ valueDecoder = (*plainDecoder)(nil)

// newPlainDecoder creates a plainDecoder that reads encoded strings from r.
func newPlainDecoder(r streamio.Reader) *plainDecoder {
	return &plainDecoder{r: r}
}

// ValueType returns [datasetmd.VALUE_TYPE_STRING].
func (dec *plainDecoder) ValueType() datasetmd.ValueType {
	return datasetmd.VALUE_TYPE_STRING
}

// EncodingType returns [datasetmd.ENCODING_TYPE_PLAIN].
func (dec *plainDecoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_PLAIN
}

// Decode decodes a string.
func (dec *plainDecoder) Decode() (Value, error) {
	sz, err := binary.ReadUvarint(dec.r)
	if err != nil {
		return StringValue(""), err
	}

	dst := make([]byte, int(sz))
	if _, err := io.ReadFull(dec.r, dst); err != nil {
		return StringValue(""), err
	}
	return StringValue(string(dst)), nil
}

// Reset implements [valueDecoder]. It resets the decoder to read from r.
func (dec *plainDecoder) Reset(r streamio.Reader) {
	dec.r = r
}
