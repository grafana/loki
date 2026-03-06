//go:build !purego

package bitpack

import (
	"github.com/parquet-go/bitpack/unsafecast"
)

//go:noescape
func unpackInt32Default(dst []int32, src []byte, bitWidth uint)

//go:noescape
func unpackInt32x1to16bitsARM64(dst []int32, src []byte, bitWidth uint)

//go:noescape
func unpackInt32x1bitNEON(dst []int32, src []byte, bitWidth uint)

//go:noescape
func unpackInt32x2bitNEON(dst []int32, src []byte, bitWidth uint)

//go:noescape
func unpackInt32x3bitNEON(dst []int32, src []byte, bitWidth uint)

//go:noescape
func unpackInt32x4bitNEON(dst []int32, src []byte, bitWidth uint)

//go:noescape
func unpackInt32x8bitNEON(dst []int32, src []byte, bitWidth uint)

func unpackInt32(dst []int32, src []byte, bitWidth uint) {
	switch {
	case bitWidth == 1:
		unpackInt32x1bitNEON(dst, src, bitWidth)
	case bitWidth == 2:
		unpackInt32x2bitNEON(dst, src, bitWidth)
	case bitWidth == 4:
		unpackInt32x4bitNEON(dst, src, bitWidth)
	case bitWidth == 8:
		unpackInt32x8bitNEON(dst, src, bitWidth)
	// bitWidth == 3,5,6,7: Skip NEON table (don't divide evenly into 8)
	case bitWidth <= 16:
		unpackInt32x1to16bitsARM64(dst, src, bitWidth)
	case bitWidth == 32:
		copy(dst, unsafecast.Slice[int32](src))
	default:
		unpackInt32Default(dst, src, bitWidth)
	}
}
