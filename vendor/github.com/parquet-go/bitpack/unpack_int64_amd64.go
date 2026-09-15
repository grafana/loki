//go:build !purego && !goexperiment.simd

package bitpack

import (
	"github.com/parquet-go/bitpack/unsafecast"
	"golang.org/x/sys/cpu"
)

//go:noescape
func unpackInt64x9to31bitsAVX2(dst []int64, src []byte, bitWidth uint)

//go:noescape
func unpackInt64x8bitsAVX2(dst []int64, src []byte)

//go:noescape
func unpackInt64x16bitsAVX2(dst []int64, src []byte)

//go:noescape
func unpackInt64x32bitsAVX2(dst []int64, src []byte)

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
