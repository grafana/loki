//go:build purego || !amd64

package rle

func encodeBytesBitpack(dst []byte, src []uint64, bitWidth uint) int {
	return encodeBytesBitpackDefault(dst, src, bitWidth)
}

func encodeInt32IndexEqual8Contiguous(words [][8]int32) (n int) {
	for n < len(words) && words[n] != broadcast8x4(words[n][0]) {
		n++
	}
	return n
}

func encodeInt32Bitpack(dst []byte, src [][8]int32, bitWidth uint) int {
	return encodeInt32BitpackDefault(dst, src, bitWidth)
}

func decodeBytesBitpack(dst, src []byte, count, bitWidth uint) {
	decodeBytesBitpackDefault(dst, src, count, bitWidth)
}
