//go:build !purego

package bitpack

//go:noescape
func packInt32ARM64(dst []byte, src []int32, bitWidth uint)

//go:noescape
func packInt32NEON(dst []byte, src []int32, bitWidth uint)

//go:noescape
func packInt64ARM64(dst []byte, src []int64, bitWidth uint)

//go:noescape
func packInt64NEON(dst []byte, src []int64, bitWidth uint)

func packInt32(dst []byte, src []int32, bitWidth uint) {
	if bitWidth <= 8 {
		packInt32NEON(dst, src, bitWidth)
	} else {
		packInt32ARM64(dst, src, bitWidth)
	}
}

func packInt64(dst []byte, src []int64, bitWidth uint) {
	if bitWidth <= 8 {
		packInt64NEON(dst, src, bitWidth)
	} else {
		packInt64ARM64(dst, src, bitWidth)
	}
}
