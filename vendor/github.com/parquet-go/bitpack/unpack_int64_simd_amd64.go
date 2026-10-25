//go:build goexperiment.simd

package bitpack

import (
	"encoding/binary"
	"simd/archsimd"
	"unsafe"

	"github.com/parquet-go/bitpack/unsafecast"
)

// This file provides implementations of the int64 bit unpacking algorithms
// based on the simd/archsimd package, replacing the hand-written assembly of
// unpack_int64_amd64.s and unpack_int64_{1,2,4,8}bit_amd64.s when
// GOEXPERIMENT=simd is set.
//
// Bit widths 8, 16 and 32 use widening loads (VPMOVZXBQ/WQ/DQ) like the
// assembly. Other bit widths up to 32 use the vectorized algorithm of the
// assembly: a cross-lane 32 bit permutation (VPERMD) places the two words
// containing each value in a 64 bit lane, giving a window that always
// contains the full value, and a per-lane logical right shift + mask
// extracts it.
//
// Bit widths 33 to 63 have no vectorized equivalent in the assembly (it
// uses generated scalar kernels); here they use a variation of the VPERMD
// algorithm with two 64 bit windows per value: the low window is aligned
// down to the containing 32 bit word (so the right shift stays below 32),
// and the high window holds the two following words, shifted left by
// 64-shift to contribute the bits the low window cannot reach. 4 values of
// at most 63 bits plus the leading shift always fit the 8 words of a 32
// byte load, so each group of 4 lanes performs one load and two
// permutations. Word indices past the load wrap (VPERMD uses the low 3
// bits) but every bit they contribute lands above the bit mask or above
// bit 63 of the lane, so they never corrupt the result.
//
// Bit width 64 is a copy.

// unpackInt64Permute holds the permutation and shift vectors used to unpack
// 8 values of a given bit width from 32 bytes of input. The value at index
// i starts at bit i*bitWidth: the permutation loads words i*bitWidth/32 and
// i*bitWidth/32+1 into a 64 bit lane, aligned by shifting right by
// i*bitWidth%32.
//
// This is the formula gen_int64_masks.go uses to generate the assembly
// tables in masks_int64_amd64.s.
type unpackInt64Permute struct {
	perm0  [8]uint32
	perm1  [8]uint32
	shift0 [4]uint64
	shift1 [4]uint64
}

// unpackInt64WidePermute holds the permutation and shift vectors used to
// unpack 8 values of bit widths 33 to 63, in two groups of 4: group 1 reads
// from a second load at the byte position of value 4. Each lane extracts
// (lo >> shift) | (hi << (64 - shift)), with lo the 64 bit window at the 32
// bit word containing the value and hi the 64 bit window after it.
type unpackInt64WidePermute struct {
	permLo0 [8]uint32
	permHi0 [8]uint32
	permLo1 [8]uint32
	permHi1 [8]uint32
	sr0     [4]uint64
	sl0     [4]uint64
	sr1     [4]uint64
	sl1     [4]uint64
}

func unpackInt64(dst []int64, src []byte, bitWidth uint) {
	if bitWidth == 64 {
		copy(dst, unsafecast.Slice[int64](src))
		return
	}
	// The Unpack contract guarantees PaddingInt64 bytes of capacity after the
	// packed values; extend the length over them so that the full-width vector
	// loads of the last iteration and the word reads of the scalar tail stay
	// in bounds.
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
		unpackInt64Default(dst, src, bitWidth)
	}
}

// unpackInt64x8x16x32bits unpacks values of bit widths 8, 16 and 32 with
// zero-extending widening loads, 4 values per instruction.
func unpackInt64x8x16x32bits(dst []int64, src []byte, bitWidth uint) {
	n := (len(dst) / 8) * 8
	in := unsafe.Pointer(unsafe.SliceData(src))
	op := unsafe.Pointer(unsafe.SliceData(dst))
	// See unpackInt64x1to32bits for why the loops walk raw pointers.
	switch bitWidth {
	case 8:
		for range n / 8 {
			archsimd.LoadUint8x16((*[16]uint8)(in)).ExtendLo4ToUint64().Store((*[4]uint64)(op))
			archsimd.LoadUint8x16((*[16]uint8)(unsafe.Add(in, 4))).ExtendLo4ToUint64().Store((*[4]uint64)(unsafe.Add(op, 32)))
			in = unsafe.Add(in, 8)
			op = unsafe.Add(op, 64)
		}
	case 16:
		for range n / 8 {
			archsimd.LoadUint16x8((*[8]uint16)(in)).ExtendLo4ToUint64().Store((*[4]uint64)(op))
			archsimd.LoadUint16x8((*[8]uint16)(unsafe.Add(in, 8))).ExtendLo4ToUint64().Store((*[4]uint64)(unsafe.Add(op, 32)))
			in = unsafe.Add(in, 16)
			op = unsafe.Add(op, 64)
		}
	default:
		for range n / 8 {
			archsimd.LoadUint32x4((*[4]uint32)(in)).ExtendToUint64().Store((*[4]uint64)(op))
			archsimd.LoadUint32x4((*[4]uint32)(unsafe.Add(in, 16))).ExtendToUint64().Store((*[4]uint64)(unsafe.Add(op, 32)))
			in = unsafe.Add(in, 32)
			op = unsafe.Add(op, 64)
		}
	}
	archsimd.ClearAVXUpperBits()
	if n < len(dst) {
		unpackInt64Default(dst[n:], src[(uint(n)/8)*bitWidth:], bitWidth)
	}
}

// unpackInt64x33to63bits unpacks values of bit widths 33 to 63 in two
// groups of 4 lanes per iteration; see the file comment for the two-window
// construction.
func unpackInt64x33to63bits(dst []int64, src []byte, bitWidth uint) {
	n := (len(dst) / 8) * 8
	if n > 0 {
		m := &unpackInt64WidePermutes[bitWidth]
		permLo0 := archsimd.LoadUint32x8(&m.permLo0)
		permHi0 := archsimd.LoadUint32x8(&m.permHi0)
		permLo1 := archsimd.LoadUint32x8(&m.permLo1)
		permHi1 := archsimd.LoadUint32x8(&m.permHi1)
		sr0 := archsimd.LoadUint64x4(&m.sr0)
		sl0 := archsimd.LoadUint64x4(&m.sl0)
		sr1 := archsimd.LoadUint64x4(&m.sr1)
		sl1 := archsimd.LoadUint64x4(&m.sl1)
		bitMask := archsimd.BroadcastUint64x4(uint64(1)<<bitWidth - 1)
		g := (4 * bitWidth) / 8

		// See unpackInt64x1to32bits for why the loop walks raw pointers.
		in := unsafe.Pointer(unsafe.SliceData(src))
		op := unsafe.Pointer(unsafe.SliceData(dst))
		for range n / 8 {
			w0 := archsimd.LoadUint8x32((*[32]uint8)(in)).AsUint32x8()
			w1 := archsimd.LoadUint8x32((*[32]uint8)(unsafe.Add(in, g))).AsUint32x8()
			w0.Permute(permLo0).AsUint64x4().ShiftRight(sr0).
				Or(w0.Permute(permHi0).AsUint64x4().ShiftLeft(sl0)).
				And(bitMask).Store((*[4]uint64)(op))
			w1.Permute(permLo1).AsUint64x4().ShiftRight(sr1).
				Or(w1.Permute(permHi1).AsUint64x4().ShiftLeft(sl1)).
				And(bitMask).Store((*[4]uint64)(unsafe.Add(op, 32)))
			in = unsafe.Add(in, bitWidth)
			op = unsafe.Add(op, 64)
		}
		archsimd.ClearAVXUpperBits()
	}
	if n < len(dst) {
		unpackInt64Default(dst[n:], src[(uint(n)/8)*bitWidth:], bitWidth)
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
		unpackInt64Default(dst[i:], src[j:], bitWidth)
	}
}

// unpackInt64x1to32bits unpacks 8 values per iteration from a 32 byte load
// using a cross-lane word permutation and per-lane shifts.
func unpackInt64x1to32bits(dst []int64, src []byte, bitWidth uint) {
	n := (len(dst) / 8) * 8
	if n > 0 {
		m := &unpackInt64Permutes[bitWidth]
		perm0 := archsimd.LoadUint32x8(&m.perm0)
		perm1 := archsimd.LoadUint32x8(&m.perm1)
		shift0 := archsimd.LoadUint64x4(&m.shift0)
		shift1 := archsimd.LoadUint64x4(&m.shift1)
		bitMask := archsimd.BroadcastUint64x4(uint64(1)<<bitWidth - 1)

		// The loop walks raw pointers: slice expressions on src and dst keep
		// enough values live across the vector ops that the loop spills to
		// the stack and re-checks bounds on every iteration. Every load is in
		// bounds of the padded src length established by unpackInt64.
		in := unsafe.Pointer(unsafe.SliceData(src))
		op := unsafe.Pointer(unsafe.SliceData(dst))
		for range n / 8 {
			w := archsimd.LoadUint8x32((*[32]uint8)(in)).AsUint32x8()
			w.Permute(perm0).AsUint64x4().ShiftRight(shift0).And(bitMask).Store((*[4]uint64)(op))
			w.Permute(perm1).AsUint64x4().ShiftRight(shift1).And(bitMask).Store((*[4]uint64)(unsafe.Add(op, 32)))
			in = unsafe.Add(in, bitWidth)
			op = unsafe.Add(op, 64)
		}
		archsimd.ClearAVXUpperBits()
	}
	if n < len(dst) {
		unpackInt64Default(dst[n:], src[(uint(n)/8)*bitWidth:], bitWidth)
	}
}

func unpackInt64Default(dst []int64, src []byte, bitWidth uint) {
	words := unsafecast.Slice[uint64](src)
	bitMask := uint64(1)<<bitWidth - 1
	bitOffset := uint(0)

	for n := range dst {
		i := bitOffset / 64
		j := bitOffset % 64
		d := (words[i] >> j) & bitMask
		if j+bitWidth > 64 {
			k := 64 - j
			d |= (words[i+1] & (bitMask >> k)) << k
		}
		dst[n] = int64(d)
		bitOffset += bitWidth
	}
}
