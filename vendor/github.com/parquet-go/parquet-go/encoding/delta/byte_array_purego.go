//go:build purego || !amd64

package delta

func decodeByteArray(dst, src []byte, prefix, suffix []int32, offsets []uint32) ([]byte, []uint32, error) {
	_ = prefix[:len(suffix)]
	_ = suffix[:len(prefix)]

	var lastValue []byte
	for i := range suffix {
		n := int(suffix[i])
		p := int(prefix[i])
		if n < 0 {
			return dst, offsets, errInvalidNegativeValueLength(n)
		}
		if n > len(src) {
			return dst, offsets, errValueLengthOutOfBounds(n, len(src))
		}
		if p < 0 {
			return dst, offsets, errInvalidNegativePrefixLength(p)
		}
		if p > len(lastValue) {
			return dst, offsets, errPrefixLengthOutOfBounds(p, len(lastValue))
		}
		j := len(dst)
		offsets = append(offsets, uint32(j))
		dst = append(dst, lastValue[:p]...)
		dst = append(dst, src[:n]...)
		lastValue = dst[j:]
		src = src[n:]
	}

	return dst, append(offsets, uint32(len(dst))), nil
}

func decodeFixedLenByteArray(dst, src []byte, size int, prefix, suffix []int32) ([]byte, error) {
	_ = prefix[:len(suffix)]
	_ = suffix[:len(prefix)]

	var lastValue []byte
	for i := range suffix {
		n := int(suffix[i])
		p := int(prefix[i])
		if n < 0 {
			return dst, errInvalidNegativeValueLength(n)
		}
		if n > len(src) {
			return dst, errValueLengthOutOfBounds(n, len(src))
		}
		if p < 0 {
			return dst, errInvalidNegativePrefixLength(p)
		}
		if p > len(lastValue) {
			return dst, errPrefixLengthOutOfBounds(p, len(lastValue))
		}
		j := len(dst)
		dst = append(dst, lastValue[:p]...)
		dst = append(dst, src[:n]...)
		lastValue = dst[j:]
		src = src[n:]
	}
	return dst, nil
}
