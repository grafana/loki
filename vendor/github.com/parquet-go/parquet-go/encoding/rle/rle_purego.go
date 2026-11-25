//go:build purego || !amd64 || goexperiment.simd

package rle

// The dispatch uses variables rather than direct calls so that the
// goexperiment.simd build can substitute accelerated implementations in its
// package init; package variable initialization runs before all init
// functions, so the overrides always win.
var (
	encodeBytesBitpack               = encodeBytesBitpackDefault
	encodeInt32IndexEqual8Contiguous = encodeInt32IndexEqual8ContiguousDefault
	encodeInt32Bitpack               = encodeInt32BitpackDefault
	decodeBytesBitpack               = decodeBytesBitpackDefault
)

func encodeInt32IndexEqual8ContiguousDefault(words [][8]int32) (n int) {
	for n < len(words) && words[n] != broadcast8x4(words[n][0]) {
		n++
	}
	return n
}
