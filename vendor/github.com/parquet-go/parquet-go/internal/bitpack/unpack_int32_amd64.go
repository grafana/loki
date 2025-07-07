//go:build !purego

package bitpack

import (
	"github.com/parquet-go/parquet-go/internal/unsafecast"
	"golang.org/x/sys/cpu"
)

//go:noescape
func unpackInt32Default(dst []int32, src []byte, bitWidth uint)

//go:noescape
func unpackInt32x1to16bitsAVX2(dst []int32, src []byte, bitWidth uint)

//go:noescape
func unpackInt32x17to26bitsAVX2(dst []int32, src []byte, bitWidth uint)

//go:noescape
func unpackInt32x27to31bitsAVX2(dst []int32, src []byte, bitWidth uint)

func unpackInt32(dst []int32, src []byte, bitWidth uint) {
	hasAVX2 := cpu.X86.HasAVX2
	switch {
	case hasAVX2 && bitWidth <= 16:
		unpackInt32x1to16bitsAVX2(dst, src, bitWidth)
	case hasAVX2 && bitWidth <= 26:
		unpackInt32x17to26bitsAVX2(dst, src, bitWidth)
	case hasAVX2 && bitWidth <= 31:
		unpackInt32x27to31bitsAVX2(dst, src, bitWidth)
	case bitWidth == 32:
		copy(dst, unsafecast.Slice[int32](src))
	default:
		unpackInt32Default(dst, src, bitWidth)
	}
}
