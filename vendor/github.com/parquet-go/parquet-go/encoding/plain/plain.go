// Package plain implements the PLAIN parquet encoding.
//
// https://github.com/apache/parquet-format/blob/master/Encodings.md#plain-plain--0
package plain

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
	"github.com/parquet-go/parquet-go/internal/unsafecast"
)

const (
	ByteArrayLengthSize = 4
	MaxByteArrayLength  = math.MaxInt32
)

type Encoding struct {
	encoding.NotSupported
}

func (e *Encoding) String() string {
	return "PLAIN"
}

func (e *Encoding) Encoding() format.Encoding {
	return format.Plain
}

func (e *Encoding) EncodeBoolean(dst []byte, src []byte) ([]byte, error) {
	return append(dst[:0], src...), nil
}

func (e *Encoding) EncodeInt96(dst []byte, src []deprecated.Int96) ([]byte, error) {
	return append(dst[:0], unsafecast.Slice[byte](src)...), nil
}

func (e *Encoding) EncodeByteArray(dst []byte, src []byte, offsets []uint32) ([]byte, error) {
	dst = dst[:0]

	if len(offsets) > 0 {
		baseOffset := offsets[0]

		for _, endOffset := range offsets[1:] {
			dst = AppendByteArray(dst, src[baseOffset:endOffset:endOffset])
			baseOffset = endOffset
		}
	}

	return dst, nil
}

func (e *Encoding) EncodeFixedLenByteArray(dst []byte, src []byte, size int) ([]byte, error) {
	if size < 0 || size > encoding.MaxFixedLenByteArraySize {
		return dst[:0], encoding.Error(e, encoding.ErrInvalidArgument)
	}
	return append(dst[:0], src...), nil
}

func (e *Encoding) DecodeBoolean(dst []byte, src []byte) ([]byte, error) {
	return append(dst[:0], src...), nil
}

func (e *Encoding) DecodeInt96(dst []deprecated.Int96, src []byte) ([]deprecated.Int96, error) {
	if (len(src) % 12) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "INT96", len(src))
	}
	return append(dst[:0], unsafecast.Slice[deprecated.Int96](src)...), nil
}

func (e *Encoding) DecodeByteArray(dst []byte, src []byte, offsets []uint32) ([]byte, []uint32, error) {
	dst, offsets = dst[:0], offsets[:0]

	for i := 0; i < len(src); {
		if (len(src) - i) < ByteArrayLengthSize {
			return dst, offsets, ErrTooShort(len(src))
		}
		n := ByteArrayLength(src[i:])
		if n > (len(src) - ByteArrayLengthSize) {
			return dst, offsets, ErrTooShort(len(src))
		}
		i += ByteArrayLengthSize
		offsets = append(offsets, uint32(len(dst)))
		dst = append(dst, src[i:i+n]...)
		i += n
	}

	return dst, append(offsets, uint32(len(dst))), nil
}

func (e *Encoding) DecodeFixedLenByteArray(dst []byte, src []byte, size int) ([]byte, error) {
	if size < 0 || size > encoding.MaxFixedLenByteArraySize {
		return dst, encoding.Error(e, encoding.ErrInvalidArgument)
	}
	if (len(src) % size) != 0 {
		return dst, encoding.ErrDecodeInvalidInputSize(e, "FIXED_LEN_BYTE_ARRAY", len(src))
	}
	return append(dst[:0], src...), nil
}

func (e *Encoding) EstimateDecodeByteArraySize(src []byte) int {
	return len(src)
}

func (e *Encoding) CanDecodeInPlace() bool {
	return true
}

func Boolean(v bool) []byte { return AppendBoolean(nil, 0, v) }

func Int32(v int32) []byte { return AppendInt32(nil, v) }

func Int64(v int64) []byte { return AppendInt64(nil, v) }

func Int96(v deprecated.Int96) []byte { return AppendInt96(nil, v) }

func Float(v float32) []byte { return AppendFloat(nil, v) }

func Double(v float64) []byte { return AppendDouble(nil, v) }

func ByteArray(v []byte) []byte { return AppendByteArray(nil, v) }

func AppendBoolean(b []byte, n int, v bool) []byte {
	i := n / 8
	j := n % 8

	if cap(b) > i {
		b = b[:i+1]
	} else {
		tmp := make([]byte, i+1, 2*(i+1))
		copy(tmp, b)
		b = tmp
	}

	k := uint(j)
	x := byte(0)
	if v {
		x = 1
	}

	b[i] = (b[i] & ^(1 << k)) | (x << k)
	return b
}

func AppendInt32(b []byte, v int32) []byte {
	x := [4]byte{}
	binary.LittleEndian.PutUint32(x[:], uint32(v))
	return append(b, x[:]...)
}

func AppendInt64(b []byte, v int64) []byte {
	x := [8]byte{}
	binary.LittleEndian.PutUint64(x[:], uint64(v))
	return append(b, x[:]...)
}

func AppendInt96(b []byte, v deprecated.Int96) []byte {
	x := [12]byte{}
	binary.LittleEndian.PutUint32(x[0:4], v[0])
	binary.LittleEndian.PutUint32(x[4:8], v[1])
	binary.LittleEndian.PutUint32(x[8:12], v[2])
	return append(b, x[:]...)
}

func AppendFloat(b []byte, v float32) []byte {
	x := [4]byte{}
	binary.LittleEndian.PutUint32(x[:], math.Float32bits(v))
	return append(b, x[:]...)
}

func AppendDouble(b []byte, v float64) []byte {
	x := [8]byte{}
	binary.LittleEndian.PutUint64(x[:], math.Float64bits(v))
	return append(b, x[:]...)
}

func AppendByteArray(b, v []byte) []byte {
	length := [ByteArrayLengthSize]byte{}
	PutByteArrayLength(length[:], len(v))
	b = append(b, length[:]...)
	b = append(b, v...)
	return b
}

func AppendByteArrayString(b []byte, v string) []byte {
	length := [ByteArrayLengthSize]byte{}
	PutByteArrayLength(length[:], len(v))
	b = append(b, length[:]...)
	b = append(b, v...)
	return b
}

func AppendByteArrayLength(b []byte, n int) []byte {
	length := [ByteArrayLengthSize]byte{}
	PutByteArrayLength(length[:], n)
	return append(b, length[:]...)
}

func ByteArrayLength(b []byte) int {
	return int(binary.LittleEndian.Uint32(b))
}

func PutByteArrayLength(b []byte, n int) {
	binary.LittleEndian.PutUint32(b, uint32(n))
}

func RangeByteArray(b []byte, do func([]byte) error) (err error) {
	for len(b) > 0 {
		var v []byte
		if v, b, err = NextByteArray(b); err != nil {
			return err
		}
		if err = do(v); err != nil {
			return err
		}
	}
	return nil
}

func NextByteArray(b []byte) (v, r []byte, err error) {
	if len(b) < ByteArrayLengthSize {
		return nil, b, ErrTooShort(len(b))
	}
	n := ByteArrayLength(b)
	if n > (len(b) - ByteArrayLengthSize) {
		return nil, b, ErrTooShort(len(b))
	}
	if n > MaxByteArrayLength {
		return nil, b, ErrTooLarge(n)
	}
	n += ByteArrayLengthSize
	return b[ByteArrayLengthSize:n:n], b[n:len(b):len(b)], nil
}

func ErrTooShort(length int) error {
	return fmt.Errorf("input of length %d is too short to contain a PLAIN encoded byte array value: %w", length, io.ErrUnexpectedEOF)
}

func ErrTooLarge(length int) error {
	return fmt.Errorf("byte array of length %d is too large to be encoded", length)
}
