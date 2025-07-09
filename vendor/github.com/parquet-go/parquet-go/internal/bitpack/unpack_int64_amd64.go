//go:build !purego

package bitpack

import (
	"github.com/parquet-go/parquet-go/internal/unsafecast"
	"golang.org/x/sys/cpu"
)

//go:noescape
func unpackInt64Default(dst []int64, src []byte, bitWidth uint)

//go:noescape
func unpackInt64x1to32bitsAVX2(dst []int64, src []byte, bitWidth uint)

func unpackInt64(dst []int64, src []byte, bitWidth uint) {
	hasAVX2 := cpu.X86.HasAVX2
	switch {
	case hasAVX2 && bitWidth <= 32:
		unpackInt64x1to32bitsAVX2(dst, src, bitWidth)
	case bitWidth == 64:
		copy(dst, unsafecast.Slice[int64](src))
	default:
		unpackInt64Default(dst, src, bitWidth)
	}
}
