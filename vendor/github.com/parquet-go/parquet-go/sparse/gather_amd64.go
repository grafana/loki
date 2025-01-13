//go:build !purego

package sparse

import (
	"golang.org/x/sys/cpu"
)

func gatherBits(dst []byte, src Uint8Array) int {
	n := min(len(dst)*8, src.Len())
	i := 0

	if n >= 8 {
		i = (n / 8) * 8
		// Make sure `offset` is at least 4 bytes, otherwise VPGATHERDD may read
		// data beyond the end of the program memory and trigger a fault.
		//
		// If the boolean values do not have enough padding we must fallback to
		// the scalar algorithm to be able to load single bytes from memory.
		if src.off >= 4 && cpu.X86.HasAVX2 {
			gatherBitsAVX2(dst, src.Slice(0, i))
		} else {
			gatherBitsDefault(dst, src.Slice(0, i))
		}
	}

	for i < n {
		x := i / 8
		y := i % 8
		b := src.Index(i)
		dst[x] = ((b & 1) << y) | (dst[x] & ^(1 << y))
		i++
	}

	return n
}

func gather32(dst []uint32, src Uint32Array) int {
	n := min(len(dst), src.Len())
	i := 0

	if n >= 16 && cpu.X86.HasAVX2 {
		i = (n / 8) * 8
		gather32AVX2(dst[:i:i], src)
	}

	for i < n {
		dst[i] = src.Index(i)
		i++
	}

	return n
}

func gather64(dst []uint64, src Uint64Array) int {
	n := min(len(dst), src.Len())
	i := 0

	if n >= 8 && cpu.X86.HasAVX2 {
		i = (n / 4) * 4
		gather64AVX2(dst[:i:i], src)
	}

	for i < n {
		dst[i] = src.Index(i)
		i++
	}

	return n
}

//go:noescape
func gatherBitsAVX2(dst []byte, src Uint8Array)

//go:noescape
func gatherBitsDefault(dst []byte, src Uint8Array)

//go:noescape
func gather32AVX2(dst []uint32, src Uint32Array)

//go:noescape
func gather64AVX2(dst []uint64, src Uint64Array)

//go:noescape
func gather128(dst [][16]byte, src Uint128Array) int
