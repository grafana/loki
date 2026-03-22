//go:build s390x

package plain

import (
	"encoding/binary"
	"math"

	"github.com/parquet-go/parquet-go/encoding"
)

// TODO: optimize by doing the byte swap in the output slice instead of
// allocating a temporay buffer.

func (e *Encoding) EncodeInt32(dst []byte, src []int32) ([]byte, error) {
	srcLen := len(src)
	byteEnc := make([]byte, (srcLen * 4))
	idx := 0
	for k := range srcLen {
		binary.LittleEndian.PutUint32(byteEnc[idx:(4+idx)], uint32((src)[k]))
		idx += 4
	}
	return append(dst[:0], (byteEnc)...), nil
}

func (e *Encoding) EncodeInt64(dst []byte, src []int64) ([]byte, error) {
	srcLen := len(src)
	byteEnc := make([]byte, (srcLen * 8))
	idx := 0
	for k := range srcLen {
		binary.LittleEndian.PutUint64(byteEnc[idx:(8+idx)], uint64((src)[k]))
		idx += 8
	}
	return append(dst[:0], (byteEnc)...), nil
}

func (e *Encoding) EncodeFloat(dst []byte, src []float32) ([]byte, error) {
	srcLen := len(src)
	byteEnc := make([]byte, (srcLen * 4))
	idx := 0
	for k := range srcLen {
		binary.LittleEndian.PutUint32(byteEnc[idx:(4+idx)], math.Float32bits((src)[k]))
		idx += 4
	}
	return append(dst[:0], (byteEnc)...), nil
}

func (e *Encoding) EncodeDouble(dst []byte, src []float64) ([]byte, error) {
	srcLen := len(src)
	byteEnc := make([]byte, (srcLen * 8))
	idx := 0
	for k := range srcLen {
		binary.LittleEndian.PutUint64(byteEnc[idx:(8+idx)], math.Float64bits((src)[k]))
		idx += 8
	}
	return append(dst[:0], (byteEnc)...), nil
}

func (e *Encoding) DecodeInt32(dst []int32, src []byte) ([]int32, error) {
	if (len(src) % 4) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "INT32", len(src))
	}
	srcLen := (len(src) / 4)
	byteDec := make([]int32, srcLen)
	idx := 0
	for k := range srcLen {
		byteDec[k] = int32(binary.LittleEndian.Uint32((src)[idx:(4 + idx)]))
		idx += 4
	}
	return append(dst[:0], (byteDec)...), nil
}

func (e *Encoding) DecodeInt64(dst []int64, src []byte) ([]int64, error) {
	if (len(src) % 8) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "INT64", len(src))
	}
	srcLen := (len(src) / 8)
	byteDec := make([]int64, srcLen)
	idx := 0
	for k := range srcLen {
		byteDec[k] = int64(binary.LittleEndian.Uint64((src)[idx:(8 + idx)]))
		idx += 8
	}
	return append(dst[:0], (byteDec)...), nil
}

func (e *Encoding) DecodeFloat(dst []float32, src []byte) ([]float32, error) {
	if (len(src) % 4) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "FLOAT", len(src))
	}
	srcLen := (len(src) / 4)
	byteDec := make([]float32, srcLen)
	idx := 0
	for k := range srcLen {
		byteDec[k] = float32(math.Float32frombits(binary.LittleEndian.Uint32((src)[idx:(4 + idx)])))
		idx += 4
	}
	return append(dst[:0], (byteDec)...), nil
}

func (e *Encoding) DecodeDouble(dst []float64, src []byte) ([]float64, error) {
	if (len(src) % 8) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "DOUBLE", len(src))
	}
	srcLen := (len(src) / 8)
	byteDec := make([]float64, srcLen)
	idx := 0
	for k := range srcLen {
		byteDec[k] = float64(math.Float64frombits(binary.LittleEndian.Uint64((src)[idx:(8 + idx)])))
		idx += 8
	}
	return append(dst[:0], (byteDec)...), nil
}
