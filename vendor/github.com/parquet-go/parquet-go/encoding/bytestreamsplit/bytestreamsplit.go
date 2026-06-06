package bytestreamsplit

import (
	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// This encoder implements a version of the Byte Stream Split encoding as described
// in https://github.com/apache/parquet-format/blob/master/Encodings.md#byte-stream-split-byte_stream_split--9
type Encoding struct {
	encoding.NotSupported
}

func (e *Encoding) String() string {
	return "BYTE_STREAM_SPLIT"
}

func (e *Encoding) Encoding() format.Encoding {
	return format.ByteStreamSplit
}

func (e *Encoding) EncodeFloat(dst []byte, src []float32) ([]byte, error) {
	dst = resize(dst, 4*len(src))
	encodeFloat(dst, unsafecast.Slice[byte](src))
	return dst, nil
}

func (e *Encoding) EncodeDouble(dst []byte, src []float64) ([]byte, error) {
	dst = resize(dst, 8*len(src))
	encodeDouble(dst, unsafecast.Slice[byte](src))
	return dst, nil
}

func (e *Encoding) DecodeFloat(dst []float32, src []byte) ([]float32, error) {
	if (len(src) % 4) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "FLOAT", len(src))
	}
	buf := resize(unsafecast.Slice[byte](dst), len(src))
	decodeFloat(buf, src)
	return unsafecast.Slice[float32](buf), nil
}

func (e *Encoding) DecodeDouble(dst []float64, src []byte) ([]float64, error) {
	if (len(src) % 8) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "DOUBLE", len(src))
	}
	buf := resize(unsafecast.Slice[byte](dst), len(src))
	decodeDouble(buf, src)
	return unsafecast.Slice[float64](buf), nil
}

func (e *Encoding) EncodeInt32(dst []byte, src []int32) ([]byte, error) {
	dst = resize(dst, 4*len(src))
	encodeInt32(dst, unsafecast.Slice[byte](src))
	return dst, nil
}

func (e *Encoding) DecodeInt32(dst []int32, src []byte) ([]int32, error) {
	if (len(src) % 4) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "INT32", len(src))
	}
	buf := resize(unsafecast.Slice[byte](dst), len(src))
	decodeInt32(buf, src)
	return unsafecast.Slice[int32](buf), nil
}

func (e *Encoding) EncodeInt64(dst []byte, src []int64) ([]byte, error) {
	dst = resize(dst, 8*len(src))
	encodeInt64(dst, unsafecast.Slice[byte](src))
	return dst, nil
}

func (e *Encoding) DecodeInt64(dst []int64, src []byte) ([]int64, error) {
	if (len(src) % 8) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "INT64", len(src))
	}
	buf := resize(unsafecast.Slice[byte](dst), len(src))
	decodeInt64(buf, src)
	return unsafecast.Slice[int64](buf), nil
}

func (e *Encoding) EncodeFixedLenByteArray(dst []byte, src []byte, size int) ([]byte, error) {
	if size <= 0 || size > encoding.MaxFixedLenByteArraySize {
		return dst[:0], encoding.Error(e, encoding.ErrInvalidArgument)
	}
	if (len(src) % size) != 0 {
		return dst[:0], encoding.ErrEncodeInvalidInputSize(e, "FIXED_LEN_BYTE_ARRAY", len(src))
	}
	dst = resize(dst, len(src))
	encodeFixedLenByteArray(dst, src, size)
	return dst, nil
}

func (e *Encoding) DecodeFixedLenByteArray(dst []byte, src []byte, size int) ([]byte, error) {
	if size <= 0 || size > encoding.MaxFixedLenByteArraySize {
		return dst, encoding.Error(e, encoding.ErrInvalidArgument)
	}
	if (len(src) % size) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "FIXED_LEN_BYTE_ARRAY", len(src))
	}
	dst = resize(dst, len(src))
	decodeFixedLenByteArray(dst, src, size)
	return dst, nil
}

func resize(buf []byte, size int) []byte {
	if cap(buf) < size {
		buf = make([]byte, size, 2*size)
	} else {
		buf = buf[:size]
	}
	return buf
}
