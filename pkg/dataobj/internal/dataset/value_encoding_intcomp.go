package dataset

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/ronanh/intcomp"
)

func init() {
	// Register the encoding so instances of it can be dynamically created.
	registerValueEncoding(
		datasetmd.VALUE_TYPE_INT64,
		datasetmd.ENCODING_TYPE_INTCOMP,
		func(w streamio.Writer) valueEncoder { return newIntCompEncoder(w) },
		func(r streamio.Reader) valueDecoder { return newIntCompDecoder(r) },
	)
}

// intCompEncoder encodes int64s using a mix of delta, zigzag and bitpacking techniques.
type intCompEncoder struct {
	w streamio.Writer
	// inputBuf is a buffer for int64 values before they are compressed in a batch. The size of this buffer controls how often the encoder flushes.
	inputBuf []int64
	// compressedBuf is a re-usable slice to compress the int64 values into.
	compressedBuf []uint64
	// outputBuf is a reusable slice for the final bytes, translated from uint64s in the compressedBuf in bytes.
	outputBuf []byte
}

var _ valueEncoder = (*intCompEncoder)(nil)

// newIntCompEncoder creates a intCompEncoder that writes encoded numbers to w.
func newIntCompEncoder(w streamio.Writer) *intCompEncoder {
	var enc intCompEncoder
	enc.inputBuf = make([]int64, 0, 256)
	enc.compressedBuf = make([]uint64, 0, 256)
	enc.outputBuf = make([]byte, 300*8)
	enc.Reset(w)
	return &enc
}

// ValueType returns [datasetmd.VALUE_TYPE_INT64].
func (enc *intCompEncoder) ValueType() datasetmd.ValueType {
	return datasetmd.VALUE_TYPE_INT64
}

// EncodingType returns [datasetmd.ENCODING_TYPE_INTCOMP].
func (enc *intCompEncoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_INTCOMP
}

// Encode encodes a new value.
func (enc *intCompEncoder) Encode(v Value) error {
	if v.Type() != datasetmd.VALUE_TYPE_INT64 {
		return fmt.Errorf("intcomp: invalid value type %v", v.Type())
	}

	enc.inputBuf = append(enc.inputBuf, v.Int64())
	if len(enc.inputBuf) == cap(enc.inputBuf) {
		err := enc.Flush()
		if err != nil {
			return err
		}
	}

	return nil
}

// Flush implements [valueEncoder]. It is a no-op for intCompEncoder.
func (enc *intCompEncoder) Flush() error {
	if len(enc.inputBuf) == 0 {
		return nil
	}
	// Convert uint64 buffer to uint64s via intcomp
	enc.compressedBuf = intcomp.CompressInt64(enc.inputBuf, enc.compressedBuf)

	// Convert the compressed data to bytes
	for i, in := range enc.compressedBuf {
		binary.LittleEndian.PutUint64(enc.outputBuf[i*8:], in)
	}

	err := streamio.WriteUvarint(enc.w, uint64(len(enc.compressedBuf)*8))
	if err != nil {
		return err
	}
	_, err = enc.w.Write(enc.outputBuf[:len(enc.compressedBuf)*8])
	if err != nil {
		return err
	}
	enc.inputBuf = enc.inputBuf[:0]
	enc.compressedBuf = enc.compressedBuf[:0]
	return nil
}

// Reset resets the encoder to its initial state.
func (enc *intCompEncoder) Reset(w streamio.Writer) {
	enc.inputBuf = enc.inputBuf[:0]
	enc.compressedBuf = enc.compressedBuf[:0]
	enc.w = w
}

// intCompDecoder decodes int64s from the bytestream using a mix of delta, zigzag and bitpacking techniques.
type intCompDecoder struct {
	r streamio.Reader
	// valueBuf is a buffer for a batch of decoded int64 values.
	valueBuf []int64
	readBuf  []byte
	compBuf  []uint64
	valueIdx int
}

var _ valueDecoder = (*intCompDecoder)(nil)

// newIntCompDecoder creates a intCompDecoder that reads encoded numbers from r.
func newIntCompDecoder(r streamio.Reader) *intCompDecoder {
	var dec intCompDecoder
	dec.valueBuf = make([]int64, 0, 256)
	dec.readBuf = make([]byte, 256*64)
	dec.compBuf = make([]uint64, 300)
	dec.Reset(r)
	return &dec
}

// ValueType returns [datasetmd.VALUE_TYPE_INT64].
func (dec *intCompDecoder) ValueType() datasetmd.ValueType {
	return datasetmd.VALUE_TYPE_INT64
}

// EncodingType returns [datasetmd.ENCODING_TYPE_INTCOMP].
func (dec *intCompDecoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_INTCOMP
}

// Decode decodes up to len(s) values, storing the results into s. The
// number of decoded values is returned, followed by an error (if any).
// At the end of the stream, Decode returns 0, [io.EOF].
func (dec *intCompDecoder) Decode(s []Value) (int, error) {
	if len(s) == 0 {
		return 0, nil
	}

	var err error
	var v Value

	for i := range s {
		v, err = dec.decode()
		if errors.Is(err, io.EOF) {
			if i == 0 {
				return 0, io.EOF
			}
			return i, nil
		} else if err != nil {
			return i, err
		}
		s[i] = v
	}
	return len(s), nil
}

// decode reads the next int64 value from the stream.
func (dec *intCompDecoder) decode() (Value, error) {
	// If there are values in the value buffer, return the next value immediately.
	// Otherwise, decode a new block of 256 values from the stream.
	if dec.valueIdx < len(dec.valueBuf) {
		v := dec.valueBuf[dec.valueIdx]
		dec.valueIdx++
		return Int64Value(v), nil
	}
	// Reset the value buffer and index
	dec.valueBuf = dec.valueBuf[:0]
	dec.valueIdx = 0

	// Read a new set of values
	bufLen, err := streamio.ReadUvarint(dec.r)
	if err != nil {
		return Int64Value(0), err
	}
	_, err = dec.r.Read(dec.readBuf[:bufLen])
	if err != nil {
		return Int64Value(0), err
	}

	// Convert each 8 bytes from readBuf into uint64 values in compBuf
	numUint64s := int(bufLen) / 8
	for i := 0; i < numUint64s; i++ {
		offset := i * 8
		dec.compBuf[i] = binary.LittleEndian.Uint64(dec.readBuf[offset : offset+8])
	}

	dec.valueBuf = intcomp.UncompressInt64(dec.compBuf[:numUint64s], dec.valueBuf)

	v := dec.valueBuf[dec.valueIdx]
	dec.valueIdx++
	return Int64Value(v), nil
}

// Reset resets the intCompDecoder to its initial state.
func (dec *intCompDecoder) Reset(r streamio.Reader) {
	dec.valueBuf = dec.valueBuf[:0]
	dec.valueIdx = 0
	dec.r = r
}
