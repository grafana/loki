package delta

import (
	"bytes"
	"sort"

	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

const (
	maxLinearSearchPrefixLength = 64 // arbitrary
)

type ByteArrayEncoding struct {
	encoding.NotSupported
}

func (e *ByteArrayEncoding) String() string {
	return "DELTA_BYTE_ARRAY"
}

func (e *ByteArrayEncoding) Encoding() format.Encoding {
	return format.DeltaByteArray
}

func (e *ByteArrayEncoding) EncodeByteArray(dst []byte, src []byte, offsets []uint32) ([]byte, error) {
	prefix := getInt32Buffer()
	defer putInt32Buffer(prefix)

	length := getInt32Buffer()
	defer putInt32Buffer(length)

	totalSize := 0
	if len(offsets) > 0 {
		lastValue := ([]byte)(nil)
		baseOffset := offsets[0]

		for _, endOffset := range offsets[1:] {
			v := src[baseOffset:endOffset:endOffset]
			n := int(endOffset - baseOffset)
			p := 0
			baseOffset = endOffset

			if len(v) <= maxLinearSearchPrefixLength {
				p = linearSearchPrefixLength(lastValue, v)
			} else {
				p = binarySearchPrefixLength(lastValue, v)
			}

			prefix.values = append(prefix.values, int32(p))
			length.values = append(length.values, int32(n-p))
			lastValue = v
			totalSize += n - p
		}
	}

	dst = dst[:0]
	dst = encodeInt32(dst, prefix.values)
	dst = encodeInt32(dst, length.values)
	dst = resize(dst, len(dst)+totalSize)

	if len(offsets) > 0 {
		b := dst[len(dst)-totalSize:]
		i := int(offsets[0])
		j := 0

		_ = length.values[:len(prefix.values)]

		for k, p := range prefix.values {
			n := p + length.values[k]
			j += copy(b[j:], src[i+int(p):i+int(n)])
			i += int(n)
		}
	}

	return dst, nil
}

func (e *ByteArrayEncoding) EncodeFixedLenByteArray(dst []byte, src []byte, size int) ([]byte, error) {
	// The parquet specs say that this encoding is only supported for BYTE_ARRAY
	// values, but the reference Java implementation appears to support
	// FIXED_LEN_BYTE_ARRAY as well:
	// https://github.com/apache/parquet-java/blob/5608695f5777de1eb0899d9075ec9411cfdf31d3/parquet-column/src/main/java/org/apache/parquet/column/Encoding.java#L211
	if size < 0 || size > encoding.MaxFixedLenByteArraySize {
		return dst[:0], encoding.Error(e, encoding.ErrInvalidArgument)
	}
	if (len(src) % size) != 0 {
		return dst[:0], encoding.ErrEncodeInvalidInputSize(e, "FIXED_LEN_BYTE_ARRAY", len(src))
	}

	prefix := getInt32Buffer()
	defer putInt32Buffer(prefix)

	length := getInt32Buffer()
	defer putInt32Buffer(length)

	totalSize := 0
	lastValue := ([]byte)(nil)

	for i := size; i <= len(src); i += size {
		v := src[i-size : i : i]
		p := linearSearchPrefixLength(lastValue, v)
		n := size - p
		prefix.values = append(prefix.values, int32(p))
		length.values = append(length.values, int32(n))
		lastValue = v
		totalSize += n
	}

	dst = dst[:0]
	dst = encodeInt32(dst, prefix.values)
	dst = encodeInt32(dst, length.values)
	dst = resize(dst, len(dst)+totalSize)

	b := dst[len(dst)-totalSize:]
	i := 0
	j := 0

	for _, p := range prefix.values {
		j += copy(b[j:], src[i+int(p):i+size])
		i += size
	}

	return dst, nil
}

func (e *ByteArrayEncoding) DecodeByteArray(dst []byte, src []byte, offsets []uint32) ([]byte, []uint32, error) {
	dst, offsets = dst[:0], offsets[:0]

	prefix := getInt32Buffer()
	defer putInt32Buffer(prefix)

	suffix := getInt32Buffer()
	defer putInt32Buffer(suffix)

	var err error
	src, err = prefix.decode(src)
	if err != nil {
		return dst, offsets, e.wrapf("decoding prefix lengths: %w", err)
	}
	src, err = suffix.decode(src)
	if err != nil {
		return dst, offsets, e.wrapf("decoding suffix lengths: %w", err)
	}
	if len(prefix.values) != len(suffix.values) {
		return dst, offsets, e.wrap(errPrefixAndSuffixLengthMismatch(len(prefix.values), len(suffix.values)))
	}
	return decodeByteArray(dst, src, prefix.values, suffix.values, offsets)
}

func (e *ByteArrayEncoding) DecodeFixedLenByteArray(dst []byte, src []byte, size int) ([]byte, error) {
	dst = dst[:0]

	if size < 0 || size > encoding.MaxFixedLenByteArraySize {
		return dst, e.wrap(encoding.ErrInvalidArgument)
	}

	prefix := getInt32Buffer()
	defer putInt32Buffer(prefix)

	suffix := getInt32Buffer()
	defer putInt32Buffer(suffix)

	var err error
	src, err = prefix.decode(src)
	if err != nil {
		return dst, e.wrapf("decoding prefix lengths: %w", err)
	}
	src, err = suffix.decode(src)
	if err != nil {
		return dst, e.wrapf("decoding suffix lengths: %w", err)
	}
	if len(prefix.values) != len(suffix.values) {
		return dst, e.wrap(errPrefixAndSuffixLengthMismatch(len(prefix.values), len(suffix.values)))
	}
	return decodeFixedLenByteArray(dst[:0], src, size, prefix.values, suffix.values)
}

func (e *ByteArrayEncoding) EstimateDecodeByteArraySize(src []byte) int {
	length := getInt32Buffer()
	defer putInt32Buffer(length)
	src, _ = length.decode(src)
	sum := int(length.sum())
	length.decode(src)
	return sum + int(length.sum())
}

func (e *ByteArrayEncoding) wrap(err error) error {
	if err != nil {
		err = encoding.Error(e, err)
	}
	return err
}

func (e *ByteArrayEncoding) wrapf(msg string, args ...interface{}) error {
	return encoding.Errorf(e, msg, args...)
}

func linearSearchPrefixLength(base, data []byte) (n int) {
	for n < len(base) && n < len(data) && base[n] == data[n] {
		n++
	}
	return n
}

func binarySearchPrefixLength(base, data []byte) int {
	n := len(base)
	if n > len(data) {
		n = len(data)
	}
	return sort.Search(n, func(i int) bool {
		return !bytes.Equal(base[:i+1], data[:i+1])
	})
}
