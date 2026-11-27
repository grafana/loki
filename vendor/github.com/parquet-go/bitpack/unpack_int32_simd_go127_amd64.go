//go:build goexperiment.simd && go1.27

package bitpack

import (
	"simd/archsimd"
	"unsafe"
)

// This file is the Go 1.27 port of unpack_int32_simd_amd64.go. Go 1.27 names
// the array-pointer load and store intrinsics Load*Array and StoreArray, and
// replaces the AsUint* bitcasts with ReshapeToUint*s. ShiftLeft and
// ShiftRight still take a vector of per-lane counts. Only bit widths 27-31
// use these kernels; widths 1-26 are slower than the assembly and stay there.
// purego has no assembly, so it still uses the archsimd kernels for every width.
//
// This file provides implementations of the int32 bit unpacking algorithms
// based on the simd/archsimd package, replacing the hand-written assembly of
// unpack_int32_amd64.s when GOEXPERIMENT=simd is set.
//
// The algorithms are adaptations of the "horizontal" method described in
// "Decoding billions of integers per second through vectorization" by
// D. Lemire & L. Boytsov (see unpack_int32_amd64.s for the full history):
// each iteration unpacks 8 values by dispatching the packed bytes into the
// four byte positions of each 32 bit lane (VPSHUFB), aligning the values on
// the lane boundaries with per-lane logical shifts (VPSRLVD/VPSLLVD), and
// masking off the extra high bits.
//
// The port differs from the assembly in how lanes 4-7 read their input: the
// assembly loads two consecutive 16 byte registers and merges two shuffles
// (bit widths 17 and up straddle the boundary between the two), while here
// the second 16 byte load is offset by the byte position of value 4, so the
// bytes of lanes 4-7 always fit a single shuffle of that register. This
// drops one shuffle+or from the 17-26 bit path and two from the 27-31 bit
// path, at the cost of the loads overlapping for bit widths below 32.
//
// The assembly loads its shuffle masks and shift vectors from tables
// declared in masks_int32_amd64.s; the equivalent tables here are computed
// once at package initialization from the layout formulas, indexed by bit
// width.

// unpackInt32Shuffle holds the shuffle masks and shift vectors used to
// unpack 8 values of a given bit width.
//
// The value at index i starts at bit i*bitWidth: in byte i*bitWidth/8 of
// the input, offset by shift = i*bitWidth%8 bits. Lanes 0-3 index their
// bytes in src[j:j+16]; lanes 4-7 in src[j+g:j+g+16] where g is the byte
// position of value 4 (g = 4*bitWidth/8), which keeps every index below 16
// for all bit widths up to 31.
//
// For bit widths 27 to 31, a value plus its leading shift may span 5 bytes,
// one more than a 32 bit lane can hold. The 4 low bytes are shifted right
// into place, and the 5th byte is dispatched to the top byte of a second
// lane which is shifted left by 8-shift, so its bits land at position
// 32-shift.
type unpackInt32Shuffle struct {
	lo0 [16]int8 // low word bytes of lanes 0-3, indexed in src[j:]
	lo1 [16]int8 // low word bytes of lanes 4-7, indexed in src[j+g:]
	hi0 [16]int8 // 5th byte of lanes 0-3, indexed in src[j:]
	hi1 [16]int8 // 5th byte of lanes 4-7, indexed in src[j+g:]
	sr0 [4]uint32
	sr1 [4]uint32
	sl0 [4]uint32
	sl1 [4]uint32
}

// unpackInt32x27to31bits unpacks values of bit widths 27 to 31, which may
// span 5 bytes: the low 4 bytes are shifted right into position, and the
// 5th byte is shifted left by 8-shift to contribute the bits above
// 32-shift.
func unpackInt32x27to31bits(dst []int32, src []byte, bitWidth uint) {
	n := (len(dst) / 8) * 8
	if n > 0 {
		m := &unpackInt32Shuffles[bitWidth]
		lo0 := archsimd.LoadInt8x16Array(&m.lo0)
		lo1 := archsimd.LoadInt8x16Array(&m.lo1)
		hi0 := archsimd.LoadInt8x16Array(&m.hi0)
		hi1 := archsimd.LoadInt8x16Array(&m.hi1)
		sr0 := archsimd.LoadUint32x4Array(&m.sr0)
		sr1 := archsimd.LoadUint32x4Array(&m.sr1)
		sl0 := archsimd.LoadUint32x4Array(&m.sl0)
		sl1 := archsimd.LoadUint32x4Array(&m.sl1)
		bitMask := archsimd.BroadcastUint32x4(uint32(1)<<bitWidth - 1)
		g := uintptr((4 * bitWidth) / 8)

		// See unpackInt32x17to26bits for why the loop walks raw offsets.
		in := unsafe.Pointer(unsafe.SliceData(src))
		op := unsafe.Pointer(unsafe.SliceData(dst))
		o := uintptr(0)
		for range n / 8 {
			w0 := archsimd.LoadUint8x16Array((*[16]uint8)(in))
			w1 := archsimd.LoadUint8x16Array((*[16]uint8)(unsafe.Add(in, g)))
			w0.PermuteOrZero(lo0).ReshapeToUint32s().ShiftRight(sr0).
				Or(w0.PermuteOrZero(hi0).ReshapeToUint32s().ShiftLeft(sl0)).
				And(bitMask).StoreArray((*[4]uint32)(unsafe.Add(op, o)))
			w1.PermuteOrZero(lo1).ReshapeToUint32s().ShiftRight(sr1).
				Or(w1.PermuteOrZero(hi1).ReshapeToUint32s().ShiftLeft(sl1)).
				And(bitMask).StoreArray((*[4]uint32)(unsafe.Add(op, o+16)))
			in = unsafe.Add(in, bitWidth)
			o += 32
		}
	}
	if n < len(dst) {
		unpackInt32Scalar(dst[n:], src[(uint(n)/8)*bitWidth:], bitWidth)
	}
}

func unpackInt32Scalar(dst []int32, src []byte, bitWidth uint) {
	bits := unsafecastBytesToUint32(src)
	bitMask := uint32(1<<bitWidth) - 1
	bitOffset := uint(0)

	for n := range dst {
		i := bitOffset / 32
		j := bitOffset % 32
		d := (bits[i] & (bitMask << j)) >> j
		if j+bitWidth > 32 {
			k := 32 - j
			d |= (bits[i+1] & (bitMask >> k)) << k
		}
		dst[n] = int32(d)
		bitOffset += bitWidth
	}
}
