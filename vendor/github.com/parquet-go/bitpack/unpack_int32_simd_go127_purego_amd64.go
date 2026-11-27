//go:build goexperiment.simd && go1.27 && purego

package bitpack

import (
	"simd/archsimd"
	"unsafe"

	"github.com/parquet-go/bitpack/unsafecast"
)

// purego builds have no assembly. Kernels that lose to the assembly are
// still faster than the scalar fallback, so every width uses archsimd here.

func unpackInt32(dst []int32, src []byte, bitWidth uint) {
	if bitWidth == 32 {
		copy(dst, unsafecast.Slice[int32](src))
		return
	}
	src = src[:ByteCount(bitWidth*uint(len(dst)))+PaddingInt32]
	hasAVX2 := archsimd.X86.AVX2()
	switch {
	case hasAVX2 && 1 <= bitWidth && bitWidth <= 16:
		unpackInt32x1to16bits(dst, src, bitWidth)
	case hasAVX2 && bitWidth <= 26:
		unpackInt32x17to26bits(dst, src, bitWidth)
	case hasAVX2 && bitWidth <= 31:
		unpackInt32x27to31bits(dst, src, bitWidth)
	default:
		unpackInt32Scalar(dst, src, bitWidth)
	}
}

// unpackInt32x1to16bits unpacks values of bit widths 1 to 16, for which the
// 8 values of an iteration fit in a single 16 byte load.
func unpackInt32x1to16bits(dst []int32, src []byte, bitWidth uint) {
	n := (len(dst) / 8) * 8
	if n > 0 {
		m := &unpackInt32Shuffles[bitWidth]
		lo0 := archsimd.LoadInt8x16Array(&m.lo0)
		lo1 := archsimd.LoadInt8x16Array(&m.lo1)
		sr0 := archsimd.LoadUint32x4Array(&m.sr0)
		sr1 := archsimd.LoadUint32x4Array(&m.sr1)
		bitMask := archsimd.BroadcastUint32x4(uint32(1)<<bitWidth - 1)

		// See unpackInt32x17to26bits for why the loop walks raw offsets.
		in := unsafe.Pointer(unsafe.SliceData(src))
		op := unsafe.Pointer(unsafe.SliceData(dst))
		o := uintptr(0)
		for range n / 8 {
			w0 := archsimd.LoadUint8x16Array((*[16]uint8)(in))
			w0.PermuteOrZero(lo0).ReshapeToUint32s().ShiftRight(sr0).And(bitMask).StoreArray((*[4]uint32)(unsafe.Add(op, o)))
			w0.PermuteOrZero(lo1).ReshapeToUint32s().ShiftRight(sr1).And(bitMask).StoreArray((*[4]uint32)(unsafe.Add(op, o+16)))
			in = unsafe.Add(in, bitWidth)
			o += 32
		}
	}
	if n < len(dst) {
		unpackInt32Scalar(dst[n:], src[(uint(n)/8)*bitWidth:], bitWidth)
	}
}

// unpackInt32x17to26bits unpacks values of bit widths 17 to 26, for which a
// value plus its leading shift spans at most 4 bytes: one shuffle + shift +
// mask per group of 4 lanes, with lanes 4-7 reading from a second load
// offset by the byte position of value 4.
func unpackInt32x17to26bits(dst []int32, src []byte, bitWidth uint) {
	n := (len(dst) / 8) * 8
	if n > 0 {
		m := &unpackInt32Shuffles[bitWidth]
		lo0 := archsimd.LoadInt8x16Array(&m.lo0)
		lo1 := archsimd.LoadInt8x16Array(&m.lo1)
		sr0 := archsimd.LoadUint32x4Array(&m.sr0)
		sr1 := archsimd.LoadUint32x4Array(&m.sr1)
		bitMask := archsimd.BroadcastUint32x4(uint32(1)<<bitWidth - 1)
		g := uintptr((4 * bitWidth) / 8)

		// The loop walks a raw pointer on the input and a raw offset on the
		// output: slice expressions on src and dst keep enough values live
		// across the vector ops that the loop spills to the stack and
		// re-checks bounds on every iteration. The input cursor may advance
		// because the padded src length established by unpackInt32 keeps its
		// final value inside the slice; the output has no padding, so an
		// advancing cursor would be one-past-end after the last iteration,
		// which checkptr rejects and the garbage collector may fault on —
		// the stores index a fixed base pointer instead.
		in := unsafe.Pointer(unsafe.SliceData(src))
		op := unsafe.Pointer(unsafe.SliceData(dst))
		o := uintptr(0)
		for range n / 8 {
			w0 := archsimd.LoadUint8x16Array((*[16]uint8)(in))
			w1 := archsimd.LoadUint8x16Array((*[16]uint8)(unsafe.Add(in, g)))
			w0.PermuteOrZero(lo0).ReshapeToUint32s().ShiftRight(sr0).And(bitMask).StoreArray((*[4]uint32)(unsafe.Add(op, o)))
			w1.PermuteOrZero(lo1).ReshapeToUint32s().ShiftRight(sr1).And(bitMask).StoreArray((*[4]uint32)(unsafe.Add(op, o+16)))
			in = unsafe.Add(in, bitWidth)
			o += 32
		}
	}
	if n < len(dst) {
		unpackInt32Scalar(dst[n:], src[(uint(n)/8)*bitWidth:], bitWidth)
	}
}
