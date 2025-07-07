//go:build !s390x

package plain

import (
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/internal/unsafecast"
)

func (e *Encoding) EncodeInt32(dst []byte, src []int32) ([]byte, error) {
	return append(dst[:0], unsafecast.Slice[byte](src)...), nil
}

func (e *Encoding) EncodeInt64(dst []byte, src []int64) ([]byte, error) {
	return append(dst[:0], unsafecast.Slice[byte](src)...), nil
}

func (e *Encoding) EncodeFloat(dst []byte, src []float32) ([]byte, error) {
	return append(dst[:0], unsafecast.Slice[byte](src)...), nil
}

func (e *Encoding) EncodeDouble(dst []byte, src []float64) ([]byte, error) {
	return append(dst[:0], unsafecast.Slice[byte](src)...), nil
}

func (e *Encoding) DecodeInt32(dst []int32, src []byte) ([]int32, error) {
	if (len(src) % 4) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "INT32", len(src))
	}
	return append(dst[:0], unsafecast.Slice[int32](src)...), nil
}

func (e *Encoding) DecodeInt64(dst []int64, src []byte) ([]int64, error) {
	if (len(src) % 8) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "INT64", len(src))
	}
	return append(dst[:0], unsafecast.Slice[int64](src)...), nil
}

func (e *Encoding) DecodeFloat(dst []float32, src []byte) ([]float32, error) {
	if (len(src) % 4) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "FLOAT", len(src))
	}
	return append(dst[:0], unsafecast.Slice[float32](src)...), nil
}

func (e *Encoding) DecodeDouble(dst []float64, src []byte) ([]float64, error) {
	if (len(src) % 8) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "DOUBLE", len(src))
	}
	return append(dst[:0], unsafecast.Slice[float64](src)...), nil
}
