//go:build purego || !amd64

package bitpack

func unpackInt64(dst []int64, src []byte, bitWidth uint) {
	bits := unsafecastBytesToUint32(src)
	bitMask := uint64(1<<bitWidth) - 1
	bitOffset := uint(0)

	for n := range dst {
		i := bitOffset / 32
		j := bitOffset % 32
		d := (uint64(bits[i]) & (bitMask << j)) >> j
		if j+bitWidth > 32 {
			k := 32 - j
			d |= (uint64(bits[i+1]) & (bitMask >> k)) << k
			if j+bitWidth > 64 {
				k := 64 - j
				d |= (uint64(bits[i+2]) & (bitMask >> k)) << k
			}
		}
		dst[n] = int64(d)
		bitOffset += bitWidth
	}
}
