package bitpack

import (
	"unsafe"

	"github.com/parquet-go/bitpack/unsafecast"
)

// Pack packs values from src to dst, each value is packed into the given
// bit width regardless of how many bits are needed to represent it.
func Pack[T Int](dst []byte, src []T, bitWidth uint) {
	_ = dst[:ByteCount(bitWidth*uint(len(src)))]
	switch unsafe.Sizeof(T(0)) {
	case 4:
		packInt32(dst, unsafecast.Slice[int32](src), bitWidth)
	default:
		packInt64(dst, unsafecast.Slice[int64](src), bitWidth)
	}
}
