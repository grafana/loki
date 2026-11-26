//go:build goexperiment.simd

package delta

import (
	"simd/archsimd"

	"github.com/parquet-go/bitpack/unsafecast"
)

// This file provides implementations of the DELTA_LENGTH_BYTE_ARRAY length
// kernels based on the simd/archsimd package, replacing the hand-written
// assembly of length_byte_array_amd64.s when GOEXPERIMENT=simd is set. The
// assembly deliberately stayed on SSE2 (4 lanes); these use AVX2 (8 lanes).

func encodeByteArrayLengths(lengths []int32, offsets []uint32) {
	i := 0
	switch {
	case archsimd.X86.AVX512() && len(lengths) >= 16 && len(offsets) > len(lengths):
		n := len(lengths) / 16
		ca := unsafecast.Slice[[16]uint32](offsets)[:n]
		cb := unsafecast.Slice[[16]uint32](offsets[1:])[:n]
		cl := unsafecast.Slice[[16]int32](lengths)[:n]
		for j := range cl {
			a := archsimd.LoadUint32x16Slice(ca[j][:])
			b := archsimd.LoadUint32x16Slice(cb[j][:])
			b.Sub(a).AsInt32x16().StoreSlice(cl[j][:])
		}
		i = n * 16
		archsimd.ClearAVXUpperBits()
	case archsimd.X86.AVX2() && len(lengths) >= 8 && len(offsets) > len(lengths):
		// The two loads are offset by one element, which defeats a single
		// chunk view; two chunk views (one over offsets, one over
		// offsets[1:]) turn both into constant-length loads that need no
		// bounds checks or per-iteration slice headers.
		n := len(lengths) / 8
		ca := unsafecast.Slice[[8]uint32](offsets)[:n]
		cb := unsafecast.Slice[[8]uint32](offsets[1:])[:n]
		cl := unsafecast.Slice[[8]int32](lengths)[:n]
		for j := range cl {
			a := archsimd.LoadUint32x8Slice(ca[j][:])
			b := archsimd.LoadUint32x8Slice(cb[j][:])
			b.Sub(a).AsInt32x8().StoreSlice(cl[j][:])
		}
		i = len(cl) * 8
		archsimd.ClearAVXUpperBits()
	}
	for ; i < len(lengths); i++ {
		lengths[i] = int32(offsets[i+1] - offsets[i])
	}
}

// Index vectors of the prefix sum ladder: each step shifts the lanes up by
// 1, 2 or 4 positions; the lanes shifted in are zeroed with the matching
// masks below (the index value of a masked-out lane is irrelevant).
var (
	shiftLanes1 = [8]uint32{0, 0, 1, 2, 3, 4, 5, 6}
	shiftLanes2 = [8]uint32{0, 0, 0, 1, 2, 3, 4, 5}
	shiftLanes4 = [8]uint32{0, 0, 0, 0, 0, 1, 2, 3}
	laneIndexes = [8]int32{0, 1, 2, 3, 4, 5, 6, 7}

	shiftLanes1x16 = [16]uint32{0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}
	shiftLanes2x16 = [16]uint32{0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
	shiftLanes4x16 = [16]uint32{0, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	shiftLanes8x16 = [16]uint32{0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7}

	lastLanex8  = [8]uint32{7, 7, 7, 7, 7, 7, 7, 7}
	lastLanex16 = [16]uint32{15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15}
)

// decodeByteArrayLengths computes the exclusive prefix sum of lengths into
// offsets, returning the total and the first negative length if any (the
// caller reports it as a data corruption error).
//
// Unlike the assembly version, which reported any negative length as -1,
// this reports the first negative value exactly like the purego version.
func decodeByteArrayLengths(offsets []uint32, lengths []int32) (uint32, int32) {
	lastOffset := uint32(0)
	i := 0
	switch {
	case archsimd.X86.AVX512() && len(lengths) >= 16:
		// The AVX-512 tier can use the mask-from-bits constructors directly:
		// KMOVW is part of the feature set the gate guarantees, unlike at
		// the AVX2 tier below.
		zero := archsimd.BroadcastInt32x16(0)
		idx1 := archsimd.LoadUint32x16Slice(shiftLanes1x16[:])
		idx2 := archsimd.LoadUint32x16Slice(shiftLanes2x16[:])
		idx4 := archsimd.LoadUint32x16Slice(shiftLanes4x16[:])
		idx8 := archsimd.LoadUint32x16Slice(shiftLanes8x16[:])
		m1 := archsimd.Mask32x16FromBits(0xFFFE)
		m2 := archsimd.Mask32x16FromBits(0xFFFC)
		m4 := archsimd.Mask32x16FromBits(0xFFF0)
		m8 := archsimd.Mask32x16FromBits(0xFF00)
		last := archsimd.LoadUint32x16Slice(lastLanex16[:])
		// The running total stays in a vector (all lanes equal): extracting
		// it to a general purpose register and broadcasting it back every
		// chunk puts a long serial chain on the loop's critical path, while
		// the vector-resident carry costs one add (the prefix ladders
		// pipeline across chunks; only the carry is serial).
		carry := zero
		cl := unsafecast.Slice[[16]int32](lengths)
		co := unsafecast.Slice[[16]uint32](offsets)
		for j := 0; j < len(cl) && j < len(co); j, i = j+1, i+16 {
			v := archsimd.LoadInt32x16Slice(cl[j][:])
			if v.Less(zero).ToBits() != 0 {
				// A negative length: let the scalar loop locate it.
				break
			}
			s := v.Add(v.Permute(idx1).Merge(zero, m1))
			s = s.Add(s.Permute(idx2).Merge(zero, m2))
			s = s.Add(s.Permute(idx4).Merge(zero, m4))
			s = s.Add(s.Permute(idx8).Merge(zero, m8))
			ex := s.Permute(idx1).Merge(zero, m1).Add(carry)
			ex.AsUint32x16().StoreSlice(co[j][:])
			carry = carry.Add(s.Permute(last))
		}
		lastOffset = uint32(carry.GetLo().GetLo().GetElem(0))
		archsimd.ClearAVXUpperBits()
	case archsimd.X86.AVX2() && len(lengths) >= 8:
		zero := archsimd.BroadcastInt32x8(0)
		idx1 := archsimd.LoadUint32x8Slice(shiftLanes1[:])
		idx2 := archsimd.LoadUint32x8Slice(shiftLanes2[:])
		idx4 := archsimd.LoadUint32x8Slice(shiftLanes4[:])
		// The masks select the lanes NOT shifted in by each ladder step.
		// They are built with compares because Mask32x8FromBits lowers to
		// KMOVD, which requires AVX-512.
		iota8 := archsimd.LoadInt32x8Slice(laneIndexes[:])
		m1 := iota8.Greater(zero)
		m2 := iota8.Greater(archsimd.BroadcastInt32x8(1))
		m4 := iota8.Greater(archsimd.BroadcastInt32x8(3))
		last := archsimd.LoadUint32x8Slice(lastLanex8[:])
		carry := zero
		cl := unsafecast.Slice[[8]int32](lengths)
		co := unsafecast.Slice[[8]uint32](offsets)
		for j := 0; j < len(cl) && j < len(co); j, i = j+1, i+8 {
			v := archsimd.LoadInt32x8Slice(cl[j][:])
			if v.Less(zero).ToBits() != 0 {
				// A negative length: let the scalar loop locate it.
				break
			}
			// Inclusive prefix sum of the 8 lanes by shift-and-add.
			s := v.Add(v.Permute(idx1).Merge(zero, m1))
			s = s.Add(s.Permute(idx2).Merge(zero, m2))
			s = s.Add(s.Permute(idx4).Merge(zero, m4))
			// The offsets are the exclusive prefix sums plus the running
			// total: shift the inclusive sums up one lane.
			ex := s.Permute(idx1).Merge(zero, m1).Add(carry)
			ex.AsUint32x8().StoreSlice(co[j][:])
			carry = carry.Add(s.Permute(last))
		}
		lastOffset = uint32(carry.GetLo().GetElem(0))
		archsimd.ClearAVXUpperBits()
	}
	for ; i < len(lengths); i++ {
		n := lengths[i]
		if n < 0 {
			return lastOffset, n
		}
		offsets[i] = lastOffset
		lastOffset += uint32(n)
	}
	offsets[len(lengths)] = lastOffset
	return lastOffset, 0
}
