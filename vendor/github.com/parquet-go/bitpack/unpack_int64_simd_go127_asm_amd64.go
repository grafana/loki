//go:build goexperiment.simd && go1.27 && !purego

package bitpack

import (
	"github.com/parquet-go/bitpack/unsafecast"
	"golang.org/x/sys/cpu"
)

//go:noescape
func unpackInt64x9to31bitsAVX2(dst []int64, src []byte, bitWidth uint)

//go:noescape
func unpackInt64x32bitsAVX2(dst []int64, src []byte)

//go:noescape
func unpackInt64x8bitsAVX2(dst []int64, src []byte)

//go:noescape
func unpackInt64x16bitsAVX2(dst []int64, src []byte)

// unpackInt64 dispatches like the assembly implementation. Archsimd replaces
// it only where that is faster: bit widths 1-7 and 33-63. Widths 8-32 stay
// on the assembly.
func unpackInt64(dst []int64, src []byte, bitWidth uint) {
	if bitWidth == 64 {
		copy(dst, unsafecast.Slice[int64](src))
		return
	}
	if bitWidth == 0 || bitWidth > 63 || len(dst) < 8 {
		unpackInt64Default(dst, src, bitWidth)
		return
	}

	hasAVX2 := cpu.X86.HasAVX2
	if hasAVX2 && (bitWidth <= 7 || bitWidth >= 33) {
		// The vector loads read PaddingInt64 bytes past the packed input.
		padded := src[:ByteCount(bitWidth*uint(len(dst)))+PaddingInt64]
		if bitWidth <= 7 {
			unpackInt64x1to32bits(dst, padded, bitWidth)
		} else {
			unpackInt64x33to63bits(dst, padded, bitWidth)
		}
		return
	}

	useGenericAVX2 := hasAVX2 && (bitWidth >= 9 && bitWidth <= 15 || bitWidth >= 17 && bitWidth <= 31)
	if len(dst)&7 == 0 {
		switch {
		case hasAVX2 && bitWidth == 8:
			unpackInt64x8bitsAVX2(dst, src)
		case hasAVX2 && bitWidth == 16:
			unpackInt64x16bitsAVX2(dst, src)
		case hasAVX2 && bitWidth == 32:
			unpackInt64x32bitsAVX2(dst, src)
		case useGenericAVX2:
			unpackInt64x9to31bitsAVX2(dst, src, bitWidth)
		default:
			unpackInt64GeneratedAMD64(dst, src, bitWidth)
		}
		return
	}

	n := len(dst) &^ 7
	switch {
	case hasAVX2 && bitWidth == 8:
		unpackInt64x8bitsAVX2(dst[:n], src)
	case hasAVX2 && bitWidth == 16:
		unpackInt64x16bitsAVX2(dst[:n], src)
	case hasAVX2 && bitWidth == 32:
		unpackInt64x32bitsAVX2(dst[:n], src)
	case useGenericAVX2:
		unpackInt64x9to31bitsAVX2(dst[:n], src, bitWidth)
	default:
		unpackInt64GeneratedAMD64(dst[:n], src, bitWidth)
	}
	if n < len(dst) {
		unpackInt64Default(dst[n:], src[n*int(bitWidth)/8:], bitWidth)
	}
}
