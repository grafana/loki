//go:build !purego

package rle

import (
	"golang.org/x/sys/cpu"
)

var (
	encodeInt32IndexEqual8Contiguous func(words [][8]int32) int
	encodeInt32Bitpack               func(dst []byte, src [][8]int32, bitWidth uint) int
	encodeBytesBitpack               func(dst []byte, src []uint64, bitWidth uint) int
	decodeBytesBitpack               func(dst, src []byte, count, bitWidth uint)
)

func init() {
	switch {
	case cpu.X86.HasAVX2:
		encodeInt32IndexEqual8Contiguous = encodeInt32IndexEqual8ContiguousAVX2
		encodeInt32Bitpack = encodeInt32BitpackAVX2
	default:
		encodeInt32IndexEqual8Contiguous = encodeInt32IndexEqual8ContiguousSSE
		encodeInt32Bitpack = encodeInt32BitpackDefault
	}

	switch {
	case cpu.X86.HasBMI2:
		encodeBytesBitpack = encodeBytesBitpackBMI2
		decodeBytesBitpack = decodeBytesBitpackBMI2
	default:
		encodeBytesBitpack = encodeBytesBitpackDefault
		decodeBytesBitpack = decodeBytesBitpackDefault
	}
}

//go:noescape
func encodeBytesBitpackBMI2(dst []byte, src []uint64, bitWidth uint) int

//go:noescape
func encodeInt32IndexEqual8ContiguousAVX2(words [][8]int32) int

//go:noescape
func encodeInt32IndexEqual8ContiguousSSE(words [][8]int32) int

//go:noescape
func encodeInt32Bitpack1to16bitsAVX2(dst []byte, src [][8]int32, bitWidth uint) int

func encodeInt32BitpackAVX2(dst []byte, src [][8]int32, bitWidth uint) int {
	switch {
	case bitWidth == 0:
		return 0
	case bitWidth <= 16:
		return encodeInt32Bitpack1to16bitsAVX2(dst, src, bitWidth)
	default:
		return encodeInt32BitpackDefault(dst, src, bitWidth)
	}
}

//go:noescape
func decodeBytesBitpackBMI2(dst, src []byte, count, bitWidth uint)
