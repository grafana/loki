//go:build !purego && !goexperiment.simd

package bitpack

func unpackInt64Default(dst []int64, src []byte, bitWidth uint) {
	bits := unsafecastBytesToUint32(src[:cap(src)])
	bitMask := uint64(1<<bitWidth) - 1
	bitOffset := uint(0)

	for i := range dst {
		word := bitOffset / 32
		shift := bitOffset % 32
		value := (uint64(bits[word]) & (bitMask << shift)) >> shift
		if shift+bitWidth > 32 {
			k := 32 - shift
			value |= (uint64(bits[word+1]) & (bitMask >> k)) << k
			if shift+bitWidth > 64 {
				k := 64 - shift
				value |= (uint64(bits[word+2]) & (bitMask >> k)) << k
			}
		}
		dst[i] = int64(value)
		bitOffset += bitWidth
	}
}
