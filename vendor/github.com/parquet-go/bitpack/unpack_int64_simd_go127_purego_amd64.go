//go:build goexperiment.simd && go1.27 && purego

package bitpack

import (
	"encoding/binary"
	"simd/archsimd"
	"unsafe"

	"github.com/parquet-go/bitpack/unsafecast"
)

// purego builds have no assembly. Kernels that lose to the assembly are
// still faster than the scalar fallback, so every width uses archsimd here.

func unpackInt64(dst []int64, src []byte, bitWidth uint) {
	if bitWidth == 64 {
		copy(dst, unsafecast.Slice[int64](src))
		return
	}
	src = src[:ByteCount(bitWidth*uint(len(dst)))+PaddingInt64]
	hasAVX2 := archsimd.X86.AVX2()
	switch {
	case hasAVX2 && (bitWidth == 8 || bitWidth == 16 || bitWidth == 32):
		unpackInt64x8x16x32bits(dst, src, bitWidth)
	case hasAVX2 && 1 <= bitWidth && bitWidth <= 32:
		unpackInt64x1to32bits(dst, src, bitWidth)
	case hasAVX2 && 33 <= bitWidth && bitWidth <= 63:
		unpackInt64x33to63bits(dst, src, bitWidth)
	case bitWidth <= 8:
		unpackInt64x1to8bits(dst, src, bitWidth)
	default:
		unpackInt64Scalar(dst, src, bitWidth)
	}
}

// unpackInt64x1to8bits unpacks 8 values per iteration from a single 64 bit
// word with scalar shifts and masks; it is only used when AVX2 is not
// available.
func unpackInt64x1to8bits(dst []int64, src []byte, bitWidth uint) {
	bitMask := uint64(1)<<bitWidth - 1
	n := (len(dst) / 8) * 8
	i, j := 0, 0
	for i < n {
		d := dst[i : i+8 : i+8]
		w := binary.LittleEndian.Uint64(src[j:])
		d[0] = int64(w & bitMask)
		d[1] = int64((w >> (1 * bitWidth)) & bitMask)
		d[2] = int64((w >> (2 * bitWidth)) & bitMask)
		d[3] = int64((w >> (3 * bitWidth)) & bitMask)
		d[4] = int64((w >> (4 * bitWidth)) & bitMask)
		d[5] = int64((w >> (5 * bitWidth)) & bitMask)
		d[6] = int64((w >> (6 * bitWidth)) & bitMask)
		d[7] = int64((w >> (7 * bitWidth)) & bitMask)
		i += 8
		j += int(bitWidth)
	}
	if i < len(dst) {
		unpackInt64Scalar(dst[i:], src[j:], bitWidth)
	}
}

// unpackInt64x8x16x32bits unpacks values of bit widths 8, 16 and 32 with
// zero-extending widening loads, 4 values per instruction.
func unpackInt64x8x16x32bits(dst []int64, src []byte, bitWidth uint) {
	n := (len(dst) / 8) * 8
	in := unsafe.Pointer(unsafe.SliceData(src))
	op := unsafe.Pointer(unsafe.SliceData(dst))
	o := uintptr(0)
	// See unpackInt64x1to32bits for why the loops walk raw offsets.
	switch bitWidth {
	case 8:
		for range n / 8 {
			archsimd.LoadUint8x16Array((*[16]uint8)(in)).ExtendLo4ToUint64().StoreArray((*[4]uint64)(unsafe.Add(op, o)))
			archsimd.LoadUint8x16Array((*[16]uint8)(unsafe.Add(in, 4))).ExtendLo4ToUint64().StoreArray((*[4]uint64)(unsafe.Add(op, o+32)))
			in = unsafe.Add(in, 8)
			o += 64
		}
	case 16:
		for range n / 8 {
			archsimd.LoadUint16x8Array((*[8]uint16)(in)).ExtendLo4ToUint64().StoreArray((*[4]uint64)(unsafe.Add(op, o)))
			archsimd.LoadUint16x8Array((*[8]uint16)(unsafe.Add(in, 8))).ExtendLo4ToUint64().StoreArray((*[4]uint64)(unsafe.Add(op, o+32)))
			in = unsafe.Add(in, 16)
			o += 64
		}
	default:
		for range n / 8 {
			archsimd.LoadUint32x4Array((*[4]uint32)(in)).ExtendToUint64().StoreArray((*[4]uint64)(unsafe.Add(op, o)))
			archsimd.LoadUint32x4Array((*[4]uint32)(unsafe.Add(in, 16))).ExtendToUint64().StoreArray((*[4]uint64)(unsafe.Add(op, o+32)))
			in = unsafe.Add(in, 32)
			o += 64
		}
	}
	archsimd.ClearAVXUpperBits()
	if n < len(dst) {
		unpackInt64Scalar(dst[n:], src[(uint(n)/8)*bitWidth:], bitWidth)
	}
}
