package bitpack

import (
	"unsafe"

	"github.com/parquet-go/bitpack/unsafecast"
)

// PaddingInt32 is the padding expected to exist after the end of input buffers
// for the UnpackInt32 algorithm to avoid reading beyond the end of the input.
const PaddingInt32 = 16

// PaddingInt64 is the padding expected to exist after the end of input buffers
// for the UnpackInt32 algorithm to avoid reading beyond the end of the input.
const PaddingInt64 = 32

// Unpack unpacks values from src to dst, each value is unpacked from the given
// bit width regardless of how many bits are needed to represent it.
func Unpack[T Int](dst []T, src []byte, bitWidth uint) {
	sizeofT := uint(unsafe.Sizeof(T(0)))
	padding := (8 * sizeofT) / 2 // 32 bits => 16, 64 bits => 32
	_ = src[:ByteCount(bitWidth*uint(len(dst))+8*padding)]
	switch sizeofT {
	case 4:
		unpackInt32(unsafecast.Slice[int32](dst), src, bitWidth)
	default:
		unpackInt64(unsafecast.Slice[int64](dst), src, bitWidth)
	}
}
