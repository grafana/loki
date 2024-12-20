//go:build !purego

package delta

import (
	"golang.org/x/sys/cpu"
)

//go:noescape
func validatePrefixAndSuffixLengthValuesAVX2(prefix, suffix []int32, maxLength int) (totalPrefixLength, totalSuffixLength int, ok bool)

func validatePrefixAndSuffixLengthValues(prefix, suffix []int32, maxLength int) (totalPrefixLength, totalSuffixLength int, err error) {
	if cpu.X86.HasAVX2 {
		totalPrefixLength, totalSuffixLength, ok := validatePrefixAndSuffixLengthValuesAVX2(prefix, suffix, maxLength)
		if ok {
			return totalPrefixLength, totalSuffixLength, nil
		}
	}

	lastValueLength := 0

	for i := range prefix {
		p := int(prefix[i])
		n := int(suffix[i])
		if p < 0 {
			err = errInvalidNegativePrefixLength(p)
			return
		}
		if n < 0 {
			err = errInvalidNegativeValueLength(n)
			return
		}
		if p > lastValueLength {
			err = errPrefixLengthOutOfBounds(p, lastValueLength)
			return
		}
		totalPrefixLength += p
		totalSuffixLength += n
		lastValueLength = p + n
	}

	if totalSuffixLength > maxLength {
		err = errValueLengthOutOfBounds(totalSuffixLength, maxLength)
		return
	}

	return totalPrefixLength, totalSuffixLength, nil
}

//go:noescape
func decodeByteArrayOffsets(offsets []uint32, prefix, suffix []int32)

//go:noescape
func decodeByteArrayAVX2(dst, src []byte, prefix, suffix []int32) int

func decodeByteArray(dst, src []byte, prefix, suffix []int32, offsets []uint32) ([]byte, []uint32, error) {
	totalPrefixLength, totalSuffixLength, err := validatePrefixAndSuffixLengthValues(prefix, suffix, len(src))
	if err != nil {
		return dst, offsets, err
	}

	totalLength := totalPrefixLength + totalSuffixLength
	dst = resizeNoMemclr(dst, totalLength+padding)

	if size := len(prefix) + 1; cap(offsets) < size {
		offsets = make([]uint32, size)
	} else {
		offsets = offsets[:size]
	}

	_ = prefix[:len(suffix)]
	_ = suffix[:len(prefix)]
	decodeByteArrayOffsets(offsets, prefix, suffix)

	var lastValue []byte
	var i int
	var j int

	if cpu.X86.HasAVX2 && len(src) > padding {
		k := len(suffix)
		n := 0

		for k > 0 && n < padding {
			k--
			n += int(suffix[k])
		}

		if k > 0 && n >= padding {
			i = decodeByteArrayAVX2(dst, src, prefix[:k], suffix[:k])
			j = len(src) - n
			lastValue = dst[i-(int(prefix[k-1])+int(suffix[k-1])):]
			prefix = prefix[k:]
			suffix = suffix[k:]
		}
	}

	for k := range prefix {
		p := int(prefix[k])
		n := int(suffix[k])
		lastValueOffset := i
		i += copy(dst[i:], lastValue[:p])
		i += copy(dst[i:], src[j:j+n])
		j += n
		lastValue = dst[lastValueOffset:]
	}

	return dst[:totalLength], offsets, nil
}

//go:noescape
func decodeByteArrayAVX2x128bits(dst, src []byte, prefix, suffix []int32) int

func decodeFixedLenByteArray(dst, src []byte, size int, prefix, suffix []int32) ([]byte, error) {
	totalPrefixLength, totalSuffixLength, err := validatePrefixAndSuffixLengthValues(prefix, suffix, len(src))
	if err != nil {
		return dst, err
	}

	totalLength := totalPrefixLength + totalSuffixLength
	dst = resizeNoMemclr(dst, totalLength+padding)

	_ = prefix[:len(suffix)]
	_ = suffix[:len(prefix)]

	var lastValue []byte
	var i int
	var j int

	if cpu.X86.HasAVX2 && len(src) > padding {
		k := len(suffix)
		n := 0

		for k > 0 && n < padding {
			k--
			n += int(suffix[k])
		}

		if k > 0 && n >= padding {
			if size == 16 {
				i = decodeByteArrayAVX2x128bits(dst, src, prefix[:k], suffix[:k])
			} else {
				i = decodeByteArrayAVX2(dst, src, prefix[:k], suffix[:k])
			}
			j = len(src) - n
			prefix = prefix[k:]
			suffix = suffix[k:]
			if i >= size {
				lastValue = dst[i-size:]
			}
		}
	}

	for k := range prefix {
		p := int(prefix[k])
		n := int(suffix[k])
		k := i
		i += copy(dst[i:], lastValue[:p])
		i += copy(dst[i:], src[j:j+n])
		j += n
		lastValue = dst[k:]
	}

	return dst[:totalLength], nil
}
