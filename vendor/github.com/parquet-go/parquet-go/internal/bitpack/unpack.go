package bitpack

// PaddingInt32 is the padding expected to exist after the end of input buffers
// for the UnpackInt32 algorithm to avoid reading beyond the end of the input.
const PaddingInt32 = 16

// PaddingInt64 is the padding expected to exist after the end of input buffers
// for the UnpackInt32 algorithm to avoid reading beyond the end of the input.
const PaddingInt64 = 32

// UnpackInt32 unpacks 32 bit integers from src to dst.
//
// The function unpacked len(dst) integers, it panics if src is too short to
// contain len(dst) values of the given bit width.
func UnpackInt32(dst []int32, src []byte, bitWidth uint) {
	_ = src[:ByteCount(bitWidth*uint(len(dst))+8*PaddingInt32)]
	unpackInt32(dst, src, bitWidth)
}

// UnpackInt64 unpacks 64 bit integers from src to dst.
//
// The function unpacked len(dst) integers, it panics if src is too short to
// contain len(dst) values of the given bit width.
func UnpackInt64(dst []int64, src []byte, bitWidth uint) {
	_ = src[:ByteCount(bitWidth*uint(len(dst))+8*PaddingInt64)]
	unpackInt64(dst, src, bitWidth)
}
