package dataset

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
)

func init() {
	// Register the encoding so instances of it can be dynamically created.
	registerValueEncoding(
		datasetmd.VALUE_TYPE_BYTE_ARRAY,
		datasetmd.ENCODING_TYPE_PLAIN,
		func(w streamio.Writer) valueEncoder { return newPlainBytesEncoder(w) },
		func(r streamio.Reader) valueDecoder { return newPlainBytesDecoder(r) },
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

// ValueType returns [datasetmd.VALUE_TYPE_BYTE_ARRAY].
func (enc *plainBytesEncoder) ValueType() datasetmd.ValueType {
	return datasetmd.VALUE_TYPE_BYTE_ARRAY
}

// EncodingType returns [datasetmd.ENCODING_TYPE_PLAIN].
func (enc *plainBytesEncoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_PLAIN
}

// Encode encodes an individual string value.
func (enc *plainBytesEncoder) Encode(v Value) error {
	if v.Type() != datasetmd.VALUE_TYPE_BYTE_ARRAY {
		return fmt.Errorf("plain: invalid value type %v", v.Type())
	}
	sv := v.ByteArray()

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

// plainBytesDecoder decodes byte arrays from an [streamio.Reader].
type plainBytesDecoder struct {
	r streamio.Reader
}

var _ valueDecoder = (*plainBytesDecoder)(nil)

// newPlainBytesDecoder creates a plainDecoder that reads encoded strings from r.
func newPlainBytesDecoder(r streamio.Reader) *plainBytesDecoder {
	return &plainBytesDecoder{r: r}
}

// ValueType returns [datasetmd.VALUE_TYPE_BYTE_ARRAY].
func (dec *plainBytesDecoder) ValueType() datasetmd.ValueType {
	return datasetmd.VALUE_TYPE_BYTE_ARRAY
}

// EncodingType returns [datasetmd.ENCODING_TYPE_PLAIN].
func (dec *plainBytesDecoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_PLAIN
}

// Decode decodes up to len(s) values, storing the results into s. The
// number of decoded values is returned, followed by an error (if any).
// At the end of the stream, Decode returns 0, [io.EOF].
func (dec *plainBytesDecoder) Decode(s []Value) (int, error) {
	if len(s) == 0 {
		return 0, nil
	}

	var err error

	for i := range s {
		err = dec.decode(&s[i])
		if errors.Is(err, io.EOF) {
			if i == 0 {
				return 0, io.EOF
			}
			return i, nil
		} else if err != nil {
			return i, err
		}
	}
	return len(s), nil
}

// decode decodes a string.
func (dec *plainBytesDecoder) decode(v *Value) error {
	sz, err := binary.ReadUvarint(dec.r)
	if err != nil {
		return err
	}

	dst := slicegrow.GrowToCap(v.Buffer(), int(sz))
	dst = dst[:sz]
	if _, err := io.ReadFull(dec.r, dst); err != nil {
		return err
	}

	*v = ByteArrayValue(dst)
	return nil
}

// Reset implements [valueDecoder]. It resets the decoder to read from r.
func (dec *plainBytesDecoder) Reset(r streamio.Reader) {
	dec.r = r
}
