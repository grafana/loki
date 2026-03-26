//go:build !purego

package bitpack

import (
	"github.com/parquet-go/bitpack/unsafecast"
)

//go:noescape
func unpackInt64Default(dst []int64, src []byte, bitWidth uint)

//go:noescape
func unpackInt64x1to32bitsARM64(dst []int64, src []byte, bitWidth uint)

//go:noescape
func unpackInt64x1bitNEON(dst []int64, src []byte, bitWidth uint)

//go:noescape
func unpackInt64x2bitNEON(dst []int64, src []byte, bitWidth uint)

//go:noescape
func unpackInt64x3bitNEON(dst []int64, src []byte, bitWidth uint)

//go:noescape
func unpackInt64x4bitNEON(dst []int64, src []byte, bitWidth uint)

//go:noescape
func unpackInt64x8bitNEON(dst []int64, src []byte, bitWidth uint)

func unpackInt64(dst []int64, src []byte, bitWidth uint) {
	// For ARM64, NEON (Advanced SIMD) is always available
	// Use table-based NEON operations for small bit widths
	switch {
	case bitWidth == 1:
		unpackInt64x1bitNEON(dst, src, bitWidth)
	case bitWidth == 2:
		unpackInt64x2bitNEON(dst, src, bitWidth)
	case bitWidth == 4:
		unpackInt64x4bitNEON(dst, src, bitWidth)
	case bitWidth == 8:
		unpackInt64x8bitNEON(dst, src, bitWidth)
	// bitWidth == 3,5,6,7: Skip NEON table (don't divide evenly into 8)
	case bitWidth <= 32:
		unpackInt64x1to32bitsARM64(dst, src, bitWidth)
	case bitWidth == 64:
		copy(dst, unsafecast.Slice[int64](src))
	default:
		unpackInt64Default(dst, src, bitWidth)
	}
}
