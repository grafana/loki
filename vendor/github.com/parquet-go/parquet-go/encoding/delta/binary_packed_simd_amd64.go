//go:build goexperiment.simd

package delta

import (
	"encoding/binary"
	"math/bits"

	"simd/archsimd"

	"github.com/parquet-go/bitpack/unsafecast"
)

// This file provides implementations of the DELTA_BINARY_PACKED block
// kernels based on the simd/archsimd package, replacing the hand-written
// assembly of binary_packed_amd64.s when GOEXPERIMENT=simd is set.
//
// The blocks are fixed size arrays ([128]int32 or [128]int64), so all the
// vector loads and stores use compile time constant indexes with no bounds
// checks. The delta and decode kernels keep their carries in vector
// registers (all lanes equal), and shifted vectors are built with
// ConcatPermute against the carry, following the patterns validated by the
// earlier tiers.
//
// The 1 bit mini block encoders use the compare-to-bits path (a mask IS the
// packed output); the 2 and 3-16 bits encoders fall back to the scalar
// packer for now (the assembly used PDEP for 2 bits, which archsimd does
// not expose, and the general packer is tracked as follow-up work).

func init() {
	if archsimd.X86.AVX2() {
		encodeInt32 = encodeInt32SIMD
		encodeInt64 = encodeInt64SIMD
	}
}

// Lane indexes building a vector shifted right by one element from the
// concatenation of the carry vector (all lanes equal) and the current
// chunk.
var (
	shiftInCarry16   = [16]uint32{15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30}
	shiftInCarry8    = [8]uint32{7, 8, 9, 10, 11, 12, 13, 14}
	lastLane16       = [16]uint32{15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15}
	lastLane8x64     = [8]uint64{7, 7, 7, 7, 7, 7, 7, 7}
	shiftInCarry8x64 = [8]uint64{7, 8, 9, 10, 11, 12, 13, 14}
	lastLane8x32     = [8]uint32{7, 7, 7, 7, 7, 7, 7, 7}

	// AVX2 tier tables: ConcatPermute and 64 bits lane permutes are EVEX
	// encodings, so the carry injection uses a full-cross VPERMD rotate on
	// the 32 bits view plus a blend of the carry lanes.
	rotate1x8x32 = [8]uint32{7, 0, 1, 2, 3, 4, 5, 6}
	rotate1x4x64 = [8]uint32{6, 7, 0, 1, 2, 3, 4, 5}
	lastPair8x32 = [8]uint32{6, 7, 6, 7, 6, 7, 6, 7}
)

func blockDeltaInt32SIMD(block *[blockSize]int32, lastValue int32) int32 {
	if archsimd.X86.AVX512() {
		idx := archsimd.LoadUint32x16Slice(shiftInCarry16[:])
		bcl := archsimd.LoadUint32x16Slice(lastLane16[:])
		carry := archsimd.BroadcastInt32x16(lastValue)
		for i := 0; i < blockSize; i += 16 {
			orig := archsimd.LoadInt32x16Slice(block[i : i+16])
			shifted := carry.ConcatPermute(orig, idx)
			orig.Sub(shifted).StoreSlice(block[i : i+16])
			carry = orig.Permute(bcl)
		}
		last := carry.GetLo().GetLo().GetElem(0)
		archsimd.ClearAVXUpperBits()
		return last
	}
	rot := archsimd.LoadUint32x8Slice(rotate1x8x32[:])
	bcl := archsimd.LoadUint32x8Slice(lastLane8x32[:])
	iota8 := archsimd.LoadInt32x8Slice(laneIndexes[:])
	tail := iota8.Greater(archsimd.BroadcastInt32x8(0))
	carry := archsimd.BroadcastInt32x8(lastValue)
	for i := 0; i < blockSize; i += 8 {
		orig := archsimd.LoadInt32x8Slice(block[i : i+8])
		shifted := orig.Permute(rot).Merge(carry, tail)
		orig.Sub(shifted).StoreSlice(block[i : i+8])
		carry = orig.Permute(bcl)
	}
	last := carry.GetLo().GetElem(0)
	archsimd.ClearAVXUpperBits()
	return last
}

func blockMinInt32SIMD(block *[blockSize]int32) int32 {
	if archsimd.X86.AVX512() {
		a0 := archsimd.LoadInt32x16Slice(block[0:16])
		a1 := archsimd.LoadInt32x16Slice(block[16:32])
		for i := 32; i < blockSize; i += 32 {
			a0 = a0.Min(archsimd.LoadInt32x16Slice(block[i : i+16]))
			a1 = a1.Min(archsimd.LoadInt32x16Slice(block[i+16 : i+32]))
		}
		a0 = a0.Min(a1)
		h := a0.GetLo().Min(a0.GetHi())
		q := h.GetLo().Min(h.GetHi())
		m := reduceMinInt32x4Delta(q)
		archsimd.ClearAVXUpperBits()
		return m
	}
	a0 := archsimd.LoadInt32x8Slice(block[0:8])
	a1 := archsimd.LoadInt32x8Slice(block[8:16])
	for i := 16; i < blockSize; i += 16 {
		a0 = a0.Min(archsimd.LoadInt32x8Slice(block[i : i+8]))
		a1 = a1.Min(archsimd.LoadInt32x8Slice(block[i+8 : i+16]))
	}
	a0 = a0.Min(a1)
	q := a0.GetLo().Min(a0.GetHi())
	m := reduceMinInt32x4Delta(q)
	archsimd.ClearAVXUpperBits()
	return m
}

func reduceMinInt32x4Delta(v archsimd.Int32x4) int32 {
	p := v.AsFloat32x4().SelectFromPair(2, 3, 0, 1, v.AsFloat32x4()).AsInt32x4()
	v = v.Min(p)
	p = v.AsFloat32x4().SelectFromPair(1, 0, 3, 2, v.AsFloat32x4()).AsInt32x4()
	v = v.Min(p)
	return v.GetElem(0)
}

func blockSubInt32SIMD(block *[blockSize]int32, value int32) {
	if archsimd.X86.AVX512() {
		v := archsimd.BroadcastInt32x16(value)
		for i := 0; i < blockSize; i += 16 {
			archsimd.LoadInt32x16Slice(block[i : i+16]).Sub(v).StoreSlice(block[i : i+16])
		}
		archsimd.ClearAVXUpperBits()
		return
	}
	v := archsimd.BroadcastInt32x8(value)
	for i := 0; i < blockSize; i += 8 {
		archsimd.LoadInt32x8Slice(block[i : i+8]).Sub(v).StoreSlice(block[i : i+8])
	}
	archsimd.ClearAVXUpperBits()
}

func blockBitWidthsInt32SIMD(bitWidths *[numMiniBlocks]byte, block *[blockSize]int32) {
	for i := range bitWidths {
		mb := block[i*miniBlockSize : i*miniBlockSize+miniBlockSize]
		var m uint32
		if archsimd.X86.AVX512() {
			a := archsimd.LoadUint32x16Slice(unsafecast.Slice[uint32](mb[0:16]))
			b := archsimd.LoadUint32x16Slice(unsafecast.Slice[uint32](mb[16:32]))
			a = a.Max(b)
			h := a.GetLo().Max(a.GetHi())
			q := h.GetLo().Max(h.GetHi())
			m = reduceMaxUint32x4Delta(q)
		} else {
			u := unsafecast.Slice[uint32](mb)
			a := archsimd.LoadUint32x8Slice(u[0:8]).Max(archsimd.LoadUint32x8Slice(u[8:16]))
			b := archsimd.LoadUint32x8Slice(u[16:24]).Max(archsimd.LoadUint32x8Slice(u[24:32]))
			a = a.Max(b)
			q := a.GetLo().Max(a.GetHi())
			m = reduceMaxUint32x4Delta(q)
		}
		bitWidths[i] = byte(bits.Len32(m))
	}
	archsimd.ClearAVXUpperBits()
}

// reduceMaxUint32x4Delta reduces in registers with a shuffle ladder; the
// float view is only a reinterpretation for SelectFromPair (shuffles are bit
// agnostic), the comparisons are the unsigned integer Max.
func reduceMaxUint32x4Delta(v archsimd.Uint32x4) uint32 {
	p := v.AsFloat32x4().SelectFromPair(2, 3, 0, 1, v.AsFloat32x4()).AsUint32x4()
	v = v.Max(p)
	p = v.AsFloat32x4().SelectFromPair(1, 0, 3, 2, v.AsFloat32x4()).AsUint32x4()
	v = v.Max(p)
	return v.GetElem(0)
}

func decodeBlockInt32(dst []int32, minDelta, lastValue int32) int32 {
	i := 0
	if archsimd.X86.AVX2() && len(dst) >= 8 {
		zero := archsimd.BroadcastInt32x8(0)
		md := archsimd.BroadcastInt32x8(minDelta)
		idx1 := archsimd.LoadUint32x8Slice(shiftLanes1[:])
		idx2 := archsimd.LoadUint32x8Slice(shiftLanes2[:])
		idx4 := archsimd.LoadUint32x8Slice(shiftLanes4[:])
		last := archsimd.LoadUint32x8Slice(lastLane8x32[:])
		iota8 := archsimd.LoadInt32x8Slice(laneIndexes[:])
		m1 := iota8.Greater(zero)
		m2 := iota8.Greater(archsimd.BroadcastInt32x8(1))
		m4 := iota8.Greater(archsimd.BroadcastInt32x8(3))
		carry := archsimd.BroadcastInt32x8(lastValue)
		cd := unsafecast.Slice[[8]int32](dst)
		for j := range cd {
			v := archsimd.LoadInt32x8Slice(cd[j][:]).Add(md)
			s := v.Add(v.Permute(idx1).Merge(zero, m1))
			s = s.Add(s.Permute(idx2).Merge(zero, m2))
			s = s.Add(s.Permute(idx4).Merge(zero, m4))
			out := s.Add(carry)
			out.StoreSlice(cd[j][:])
			carry = out.Permute(last)
		}
		i = len(cd) * 8
		lastValue = carry.GetLo().GetElem(0)
		archsimd.ClearAVXUpperBits()
	}
	for ; i < len(dst); i++ {
		dst[i] += minDelta + lastValue
		lastValue = dst[i]
	}
	return lastValue
}

func blockDeltaInt64SIMD(block *[blockSize]int64, lastValue int64) int64 {
	if archsimd.X86.AVX512() {
		idx := archsimd.LoadUint64x8Slice(shiftInCarry8x64[:])
		bcl := archsimd.LoadUint64x8Slice(lastLane8x64[:])
		carry := archsimd.BroadcastInt64x8(lastValue)
		for i := 0; i < blockSize; i += 8 {
			orig := archsimd.LoadInt64x8Slice(block[i : i+8])
			shifted := carry.ConcatPermute(orig, idx)
			orig.Sub(shifted).StoreSlice(block[i : i+8])
			carry = orig.Permute(bcl)
		}
		last := carry.GetLo().GetLo().GetElem(0)
		archsimd.ClearAVXUpperBits()
		return last
	}
	rot := archsimd.LoadUint32x8Slice(rotate1x4x64[:])
	bcp := archsimd.LoadUint32x8Slice(lastPair8x32[:])
	iota8 := archsimd.LoadInt32x8Slice(laneIndexes[:])
	tail := iota8.Greater(archsimd.BroadcastInt32x8(1))
	carry := archsimd.BroadcastInt64x4(lastValue)
	for i := 0; i < blockSize; i += 4 {
		orig := archsimd.LoadInt64x4Slice(block[i : i+4])
		o32 := orig.AsInt32x8()
		shifted := o32.Permute(rot).Merge(carry.AsInt32x8(), tail).AsInt64x4()
		orig.Sub(shifted).StoreSlice(block[i : i+4])
		carry = o32.Permute(bcp).AsInt64x4()
	}
	last := carry.GetLo().GetElem(0)
	archsimd.ClearAVXUpperBits()
	return last
}

func blockMinInt64SIMD(block *[blockSize]int64) int64 {
	if archsimd.X86.AVX512() {
		a0 := archsimd.LoadInt64x8Slice(block[0:8])
		a1 := archsimd.LoadInt64x8Slice(block[8:16])
		for i := 16; i < blockSize; i += 16 {
			a0 = a0.Min(archsimd.LoadInt64x8Slice(block[i : i+8]))
			a1 = a1.Min(archsimd.LoadInt64x8Slice(block[i+8 : i+16]))
		}
		a0 = a0.Min(a1)
		h := a0.GetLo().Min(a0.GetHi())
		q := h.GetLo().Min(h.GetHi())
		m := q.GetElem(0)
		if x := q.GetElem(1); x < m {
			m = x
		}
		archsimd.ClearAVXUpperBits()
		return m
	}
	// Int64x4.Min is AVX-512 only (VPMINSQ); the AVX2 tier selects with a
	// signed compare and merge.
	a0 := archsimd.LoadInt64x4Slice(block[0:4])
	a1 := archsimd.LoadInt64x4Slice(block[4:8])
	for i := 8; i < blockSize; i += 8 {
		v0 := archsimd.LoadInt64x4Slice(block[i : i+4])
		v1 := archsimd.LoadInt64x4Slice(block[i+4 : i+8])
		a0 = v0.Merge(a0, a0.Greater(v0))
		a1 = v1.Merge(a1, a1.Greater(v1))
	}
	a0 = a1.Merge(a0, a0.Greater(a1))
	lo := a0.GetLo()
	hi := a0.GetHi()
	q := hi.Merge(lo, lo.Greater(hi))
	m := q.GetElem(0)
	if x := q.GetElem(1); x < m {
		m = x
	}
	archsimd.ClearAVXUpperBits()
	return m
}

func blockSubInt64SIMD(block *[blockSize]int64, value int64) {
	if archsimd.X86.AVX512() {
		v := archsimd.BroadcastInt64x8(value)
		for i := 0; i < blockSize; i += 8 {
			archsimd.LoadInt64x8Slice(block[i : i+8]).Sub(v).StoreSlice(block[i : i+8])
		}
		archsimd.ClearAVXUpperBits()
		return
	}
	v := archsimd.BroadcastInt64x4(value)
	for i := 0; i < blockSize; i += 4 {
		archsimd.LoadInt64x4Slice(block[i : i+4]).Sub(v).StoreSlice(block[i : i+4])
	}
	archsimd.ClearAVXUpperBits()
}

func blockBitWidthsInt64SIMD(bitWidths *[numMiniBlocks]byte, block *[blockSize]int64) {
	for i := range bitWidths {
		mb := block[i*miniBlockSize : i*miniBlockSize+miniBlockSize]
		u := unsafecast.Slice[uint64](mb)
		var m uint64
		if archsimd.X86.AVX512() {
			a := archsimd.LoadUint64x8Slice(u[0:8]).Max(archsimd.LoadUint64x8Slice(u[8:16]))
			b := archsimd.LoadUint64x8Slice(u[16:24]).Max(archsimd.LoadUint64x8Slice(u[24:32]))
			a = a.Max(b)
			h := a.GetLo().Max(a.GetHi())
			q := h.GetLo().Max(h.GetHi())
			m = q.GetElem(0)
			if x := q.GetElem(1); x > m {
				m = x
			}
		} else {
			// Unsigned 64 bits max at the AVX2 tier: sign-biased signed
			// compare and merge (VPMAXUQ is AVX-512 only).
			sign := archsimd.BroadcastUint64x4(1 << 63)
			a := archsimd.LoadUint64x4Slice(u[0:4])
			for k := 4; k < miniBlockSize; k += 4 {
				v := archsimd.LoadUint64x4Slice(u[k : k+4])
				gt := v.Xor(sign).AsInt64x4().Greater(a.Xor(sign).AsInt64x4())
				a = v.Merge(a, gt)
			}
			lo := a.GetLo()
			hi := a.GetHi()
			gt := hi.Xor(sign.GetLo()).AsInt64x2().Greater(lo.Xor(sign.GetLo()).AsInt64x2())
			q := hi.Merge(lo, gt)
			m = q.GetElem(0)
			if x := q.GetElem(1); x > m {
				m = x
			}
		}
		bitWidths[i] = byte(bits.Len64(m))
	}
	archsimd.ClearAVXUpperBits()
}

func decodeBlockInt64(dst []int64, minDelta, lastValue int64) int64 {
	for i := range dst {
		dst[i] += minDelta + lastValue
		lastValue = dst[i]
	}
	return lastValue
}

// encodeMiniBlockInt32 is the scalar packer used for the widths without a
// vector specialization; it mirrors the purego implementation (the packed
// destination is zero initialized by the callers).
func encodeMiniBlockInt32(dst []byte, src *[miniBlockSize]int32, bitWidth uint) {
	bitMask := uint32(1<<bitWidth) - 1
	bitOffset := uint(0)

	for _, value := range src {
		i := bitOffset / 32
		j := bitOffset % 32

		lo := binary.LittleEndian.Uint32(dst[(i+0)*4:])
		hi := binary.LittleEndian.Uint32(dst[(i+1)*4:])

		lo |= (uint32(value) & bitMask) << j
		hi |= (uint32(value) >> (32 - j))

		binary.LittleEndian.PutUint32(dst[(i+0)*4:], lo)
		binary.LittleEndian.PutUint32(dst[(i+1)*4:], hi)

		bitOffset += bitWidth
	}
}

func encodeMiniBlockInt64(dst []byte, src *[miniBlockSize]int64, bitWidth uint) {
	bitMask := uint64(1<<bitWidth) - 1
	bitOffset := uint(0)

	for _, value := range src {
		i := bitOffset / 64
		j := bitOffset % 64

		lo := binary.LittleEndian.Uint64(dst[(i+0)*8:])
		hi := binary.LittleEndian.Uint64(dst[(i+1)*8:])

		lo |= (uint64(value) & bitMask) << j
		hi |= (uint64(value) >> (64 - j))

		binary.LittleEndian.PutUint64(dst[(i+0)*8:], lo)
		binary.LittleEndian.PutUint64(dst[(i+1)*8:], hi)

		bitOffset += bitWidth
	}
}

// The 1 bit encoders are compare-to-mask: the mask bits ARE the packed
// output.
func encodeMiniBlockInt32x1bit(dst []byte, src *[miniBlockSize]int32) {
	one := archsimd.BroadcastInt32x16(1)
	m0 := archsimd.LoadInt32x16Slice(src[0:16]).Equal(one).ToBits()
	m1 := archsimd.LoadInt32x16Slice(src[16:32]).Equal(one).ToBits()
	binary.LittleEndian.PutUint32(dst, uint32(m0)|uint32(m1)<<16)
	archsimd.ClearAVXUpperBits()
}

func encodeMiniBlockInt64x1bit(dst []byte, src *[miniBlockSize]int64) {
	one := archsimd.BroadcastInt64x8(1)
	m0 := archsimd.LoadInt64x8Slice(src[0:8]).Equal(one).ToBits()
	m1 := archsimd.LoadInt64x8Slice(src[8:16]).Equal(one).ToBits()
	m2 := archsimd.LoadInt64x8Slice(src[16:24]).Equal(one).ToBits()
	m3 := archsimd.LoadInt64x8Slice(src[24:32]).Equal(one).ToBits()
	bits := uint32(m0) | uint32(m1)<<8 | uint32(m2)<<16 | uint32(m3)<<24
	binary.LittleEndian.PutUint32(dst, bits)
	archsimd.ClearAVXUpperBits()
}

// Lane indexes folding odd lanes onto even lanes and lanes {2,6} onto
// {0,4} in the packing reduction (the values of the unused lanes are
// irrelevant, the useful results live in lanes 0 and 4 afterwards).
var (
	foldOdd  = [8]uint64{1, 1, 3, 3, 5, 5, 7, 7}
	foldPair = [8]uint64{2, 2, 2, 2, 6, 6, 6, 6}
)

// encodeMiniBlockInt32Packed packs 32 values of 2 to 16 bits each. Groups of
// 8 values span exactly bitWidth bytes, so each group stores 16 bytes at its
// byte aligned offset and the zeroed tail is overwritten by the next group
// (the callers reserve 16 bytes of headroom, like they did for the
// assembly). The 8 widened values are folded pairwise with variable shifts:
// two vector steps leave 4 values in lane 0 and 4 in lane 4, and a scalar
// 128 bits stitch combines them.
func encodeMiniBlockInt32Packed(dst []byte, src *[miniBlockSize]int32, bitWidth uint) {
	w := uint64(bitWidth)
	var sh1v [8]uint64
	var sh2v [8]uint64
	for i := range sh1v {
		if i%2 == 1 {
			sh1v[i] = w
		}
		if i == 2 || i == 6 {
			sh2v[i] = 2 * w
		}
	}
	sh1 := archsimd.LoadUint64x8Slice(sh1v[:])
	sh2 := archsimd.LoadUint64x8Slice(sh2v[:])
	fo := archsimd.LoadUint64x8Slice(foldOdd[:])
	fp := archsimd.LoadUint64x8Slice(foldPair[:])
	u := unsafecast.Slice[uint32](src[:])
	off := uint(0)
	for g := 0; g < miniBlockSize; g += 8 {
		v := archsimd.LoadUint32x8Slice(u[g : g+8]).ExtendToUint64()
		t := v.ShiftLeft(sh1)
		t = t.Or(t.Permute(fo))
		t = t.ShiftLeft(sh2)
		t = t.Or(t.Permute(fp))
		q0 := t.GetLo().GetLo().GetElem(0)
		q1 := t.GetHi().GetLo().GetElem(0)
		lo := q0 | q1<<(4*w)
		hi := q1 >> (64 - 4*w)
		binary.LittleEndian.PutUint64(dst[off:], lo)
		binary.LittleEndian.PutUint64(dst[off+8:], hi)
		off += uint(w)
	}
	archsimd.ClearAVXUpperBits()
}

var (
	packLowBytes64 = [64]uint8{0, 8, 16, 24, 32, 40, 48, 56, 64, 72, 80, 88, 96, 104, 112, 120, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	packLowBytes32 = [64]uint8{0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 44, 48, 52, 56, 60, 64, 68, 72, 76, 80, 84, 88, 92, 96, 100, 104, 108, 112, 116, 120, 124, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	packHalves     = [64]uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	packDwordLow   = [64]uint8{0, 4, 8, 12, 16, 20, 24, 28, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	packMadd1      = [64]int8{1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4, 1, 4}
	packMadd2      = [64]int16{1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16, 1, 16}
)

// encodeMiniBlockInt32x2bits packs 32 2 bit values using two multiply add
// pairs steps instead of the PDEP instructions the assembly used: the low
// bytes of the values are gathered with byte permutes, VPMADDUBSW packs
// byte pairs into 4 bit nibbles and VPMADDWD packs nibble pairs into bytes.
func encodeMiniBlockInt32x2bits(dst []byte, src *[miniBlockSize]int32) {
	lo := archsimd.LoadUint8x64Slice(packLowBytes32[:])
	dl := archsimd.LoadUint8x64Slice(packDwordLow[:])
	m1 := archsimd.LoadInt8x64Slice(packMadd1[:])
	m2 := archsimd.LoadInt16x32Slice(packMadd2[:])
	u := unsafecast.Slice[uint8](src[:])
	z0 := archsimd.LoadUint8x64Slice(u[0:64])
	z1 := archsimd.LoadUint8x64Slice(u[64:128])
	b := z0.ConcatPermute(z1, lo)
	w := b.DotProductPairsSaturated(m1)
	d := w.DotProductPairs(m2)
	q := d.AsUint8x64().Permute(dl).AsUint64x8().GetLo().GetLo().GetElem(0)
	binary.LittleEndian.PutUint64(dst, q)
	archsimd.ClearAVXUpperBits()
}

// encodeMiniBlockInt64x2bits is the 64 bit variant of the multiply add
// pairs packer: the same reduction after gathering the low bytes of the 32
// values from 4 vectors instead of 2.
func encodeMiniBlockInt64x2bits(dst []byte, src *[miniBlockSize]int64) {
	lo := archsimd.LoadUint8x64Slice(packLowBytes64[:])
	hv := archsimd.LoadUint8x64Slice(packHalves[:])
	dl := archsimd.LoadUint8x64Slice(packDwordLow[:])
	m1 := archsimd.LoadInt8x64Slice(packMadd1[:])
	m2 := archsimd.LoadInt16x32Slice(packMadd2[:])
	u := unsafecast.Slice[uint8](src[:])
	z0 := archsimd.LoadUint8x64Slice(u[0:64])
	z1 := archsimd.LoadUint8x64Slice(u[64:128])
	z2 := archsimd.LoadUint8x64Slice(u[128:192])
	z3 := archsimd.LoadUint8x64Slice(u[192:256])
	b := z0.ConcatPermute(z1, lo).ConcatPermute(z2.ConcatPermute(z3, lo), hv)
	w := b.DotProductPairsSaturated(m1)
	d := w.DotProductPairs(m2)
	q := d.AsUint8x64().Permute(dl).AsUint64x8().GetLo().GetLo().GetElem(0)
	binary.LittleEndian.PutUint64(dst, q)
	archsimd.ClearAVXUpperBits()
}

func encodeMiniBlockInt32SIMD(dst []byte, src *[miniBlockSize]int32, bitWidth uint) {
	switch {
	case bitWidth == 1 && archsimd.X86.AVX512():
		encodeMiniBlockInt32x1bit(dst, src)
	case bitWidth == 2 && archsimd.X86.AVX512() && archsimd.X86.AVX512VBMI():
		encodeMiniBlockInt32x2bits(dst, src)
	case bitWidth >= 2 && bitWidth <= 16 && archsimd.X86.AVX512():
		encodeMiniBlockInt32Packed(dst, src, bitWidth)
	case bitWidth == 32:
		copy(dst, unsafecast.Slice[byte](src[:]))
	default:
		encodeMiniBlockInt32(dst, src, bitWidth)
	}
}

// encodeMiniBlockInt64Packed packs 32 values of 2 to 16 bits each, using the
// same fold reduction as encodeMiniBlockInt32Packed: the values are already
// 64 bits wide so the widening step disappears. The assembly only had a PDEP
// fast path for 2 bits and fell back to scalar for everything else, so this
// covers more widths than it did.
func encodeMiniBlockInt64Packed(dst []byte, src *[miniBlockSize]int64, bitWidth uint) {
	w := uint64(bitWidth)
	var sh1v [8]uint64
	var sh2v [8]uint64
	for i := range sh1v {
		if i%2 == 1 {
			sh1v[i] = w
		}
		if i == 2 || i == 6 {
			sh2v[i] = 2 * w
		}
	}
	sh1 := archsimd.LoadUint64x8Slice(sh1v[:])
	sh2 := archsimd.LoadUint64x8Slice(sh2v[:])
	fo := archsimd.LoadUint64x8Slice(foldOdd[:])
	fp := archsimd.LoadUint64x8Slice(foldPair[:])
	u := unsafecast.Slice[uint64](src[:])
	off := uint(0)
	for g := 0; g < miniBlockSize; g += 8 {
		t := archsimd.LoadUint64x8Slice(u[g : g+8]).ShiftLeft(sh1)
		t = t.Or(t.Permute(fo))
		t = t.ShiftLeft(sh2)
		t = t.Or(t.Permute(fp))
		q0 := t.GetLo().GetLo().GetElem(0)
		q1 := t.GetHi().GetLo().GetElem(0)
		lo := q0 | q1<<(4*w)
		hi := q1 >> (64 - 4*w)
		binary.LittleEndian.PutUint64(dst[off:], lo)
		binary.LittleEndian.PutUint64(dst[off+8:], hi)
		off += uint(w)
	}
	archsimd.ClearAVXUpperBits()
}

func encodeMiniBlockInt64SIMD(dst []byte, src *[miniBlockSize]int64, bitWidth uint) {
	switch {
	case bitWidth == 1 && archsimd.X86.AVX512():
		encodeMiniBlockInt64x1bit(dst, src)
	case bitWidth == 2 && archsimd.X86.AVX512() && archsimd.X86.AVX512VBMI():
		encodeMiniBlockInt64x2bits(dst, src)
	case bitWidth >= 2 && bitWidth <= 16 && archsimd.X86.AVX512():
		encodeMiniBlockInt64Packed(dst, src, bitWidth)
	case bitWidth == 64:
		copy(dst, unsafecast.Slice[byte](src[:]))
	default:
		encodeMiniBlockInt64(dst, src, bitWidth)
	}
}

func encodeInt32SIMD(dst []byte, src []int32) []byte {
	totalValues := len(src)
	firstValue := int32(0)
	if totalValues > 0 {
		firstValue = src[0]
	}

	n := len(dst)
	dst = resize(dst, n+maxHeaderLength32)
	dst = dst[:n+encodeBinaryPackedHeader(dst[n:], blockSize, numMiniBlocks, totalValues, int64(firstValue))]

	if totalValues < 2 {
		return dst
	}

	lastValue := firstValue
	for i := 1; i < len(src); i += blockSize {
		block := [blockSize]int32{}
		blockLength := copy(block[:], src[i:])

		lastValue = blockDeltaInt32SIMD(&block, lastValue)
		minDelta := blockMinInt32SIMD(&block)
		blockSubInt32SIMD(&block, minDelta)
		blockClearInt32(&block, blockLength)

		bitWidths := [numMiniBlocks]byte{}
		blockBitWidthsInt32SIMD(&bitWidths, &block)

		n := len(dst)
		dst = resize(dst, n+maxMiniBlockLength32+16)
		n += encodeBlockHeader(dst[n:], int64(minDelta), bitWidths)

		for i, bitWidth := range bitWidths {
			if bitWidth != 0 {
				miniBlock := (*[miniBlockSize]int32)(block[i*miniBlockSize:])
				encodeMiniBlockInt32SIMD(dst[n:], miniBlock, uint(bitWidth))
				n += (miniBlockSize * int(bitWidth)) / 8
			}
		}

		dst = dst[:n]
	}

	return dst
}

func encodeInt64SIMD(dst []byte, src []int64) []byte {
	totalValues := len(src)
	firstValue := int64(0)
	if totalValues > 0 {
		firstValue = src[0]
	}

	n := len(dst)
	dst = resize(dst, n+maxHeaderLength64)
	dst = dst[:n+encodeBinaryPackedHeader(dst[n:], blockSize, numMiniBlocks, totalValues, firstValue)]

	if totalValues < 2 {
		return dst
	}

	lastValue := firstValue
	for i := 1; i < len(src); i += blockSize {
		block := [blockSize]int64{}
		blockLength := copy(block[:], src[i:])

		lastValue = blockDeltaInt64SIMD(&block, lastValue)
		minDelta := blockMinInt64SIMD(&block)
		blockSubInt64SIMD(&block, minDelta)
		blockClearInt64(&block, blockLength)

		bitWidths := [numMiniBlocks]byte{}
		blockBitWidthsInt64SIMD(&bitWidths, &block)

		n := len(dst)
		dst = resize(dst, n+maxMiniBlockLength64+16)
		n += encodeBlockHeader(dst[n:], minDelta, bitWidths)

		for i, bitWidth := range bitWidths {
			if bitWidth != 0 {
				miniBlock := (*[miniBlockSize]int64)(block[i*miniBlockSize:])
				encodeMiniBlockInt64SIMD(dst[n:], miniBlock, uint(bitWidth))
				n += (miniBlockSize * int(bitWidth)) / 8
			}
		}

		dst = dst[:n]
	}

	return dst
}
