//go:build purego || (!amd64 && !arm64)

package bitpack

func packInt32(dst []byte, src []int32, bitWidth uint) {
	packInt32Default(dst, src, bitWidth)
}

func packInt64(dst []byte, src []int64, bitWidth uint) {
	packInt64Default(dst, src, bitWidth)
}
