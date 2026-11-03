//go:build !purego

package bitpack

//go:generate go run gen_pack_amd64.go

import "golang.org/x/sys/cpu"

//go:noescape
func packInt32AVX2(dst []byte, src []int32, bitWidth uint)

//go:noescape
func packInt64AVX2(dst []byte, src []int64, bitWidth uint)

func packInt32(dst []byte, src []int32, bitWidth uint) {
	if bitWidth == 0 || bitWidth > 32 || len(src) < 8 {
		packInt32Default(dst, src, bitWidth)
		return
	}

	n := len(src) &^ 7
	useGenerated := bitWidth == 32 || bitWidth >= 2 && bitWidth <= 31 && bitWidth%8 != 0
	switch {
	case useGenerated:
		packInt32GeneratedAMD64(dst, src[:n], bitWidth)
	case cpu.X86.HasAVX2:
		packInt32AVX2(dst, src[:n], bitWidth)
	default:
		packInt32Default(dst, src, bitWidth)
		return
	}
	packInt32Default(dst[n*int(bitWidth)/8:], src[n:], bitWidth)
}

func packInt64(dst []byte, src []int64, bitWidth uint) {
	if bitWidth == 0 || bitWidth > 64 || len(src) < 8 {
		packInt64Default(dst, src, bitWidth)
		return
	}

	n := len(src) &^ 7
	useGenerated := bitWidth >= 33 || bitWidth >= 2 && bitWidth <= 31 && bitWidth%8 != 0
	switch {
	case useGenerated:
		packInt64GeneratedAMD64(dst, src[:n], bitWidth)
	case cpu.X86.HasAVX2:
		packInt64AVX2(dst, src[:n], bitWidth)
	default:
		packInt64Default(dst, src, bitWidth)
		return
	}
	packInt64Default(dst[n*int(bitWidth)/8:], src[n:], bitWidth)
}
