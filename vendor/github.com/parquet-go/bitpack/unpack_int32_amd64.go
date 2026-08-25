//go:build !purego && !goexperiment.simd

package bitpack

import (
	"github.com/parquet-go/bitpack/unsafecast"
	"golang.org/x/sys/cpu"
)

//go:generate go run gen_unpack_amd64.go

//go:noescape
func unpackInt32Default(dst []int32, src []byte, bitWidth uint)

//go:noescape
func unpackInt32x1to16bitsAVX2(dst []int32, src []byte, bitWidth uint)

//go:noescape
func unpackInt32x17to26bitsAVX2(dst []int32, src []byte, bitWidth uint)

//go:noescape
func unpackInt32x27to31bitsAVX2(dst []int32, src []byte, bitWidth uint)

func unpackInt32(dst []int32, src []byte, bitWidth uint) {
	if bitWidth == 32 {
		copy(dst, unsafecast.Slice[int32](src))
		return
	}
	if cpu.X86.HasAVX2 && bitWidth >= 1 && bitWidth <= 31 {
		switch {
		case bitWidth <= 16:
			unpackInt32x1to16bitsAVX2(dst, src, bitWidth)
		case bitWidth <= 26:
			unpackInt32x17to26bitsAVX2(dst, src, bitWidth)
		default:
			unpackInt32x27to31bitsAVX2(dst, src, bitWidth)
		}
		return
	}
	if bitWidth == 0 || bitWidth > 31 || len(dst) < 8 {
		unpackInt32Default(dst, src, bitWidth)
		return
	}

	n := len(dst) &^ 7
	unpackInt32GeneratedAMD64(dst[:n], src, bitWidth)
	if n < len(dst) {
		unpackInt32Default(dst[n:], src[n*int(bitWidth)/8:], bitWidth)
	}
}
