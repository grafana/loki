//go:build goexperiment.simd

package parquet

import (
	"simd/archsimd"

	"github.com/parquet-go/bitpack/unsafecast"
)

// This file provides implementations of the orderOf kernels based on the
// simd/archsimd package, replacing the hand-written assembly of
// order_amd64.s when GOEXPERIMENT=simd is set. Unlike the assembly, which
// only had AVX-512 paths, these implementations also provide AVX2 paths.
//
// The kernels detect whether values are in ascending (+1), descending (-1)
// or undefined (0) order by comparing each vector against the same values
// shifted by one element. The AVX-512 tier loads each chunk once and builds
// the shifted vector with ConcatPermute against the next chunk (the
// assembly's VPERMI2D trick); the remaining pairs are covered by one or two
// vector compares overlapping the already checked elements. The AVX2 tier
// uses two overlapping loads: ConcatPermute at 256 bits is an EVEX
// instruction, and on AVX2-only CPUs any vector path is already a gain over
// the assembly, which fell back to scalar code there.
//
// Floating point sequences containing NaN report undefined order: the
// LessEqual/GreaterEqual comparisons are false for NaN lanes, failing both
// scans (matching the assembly's VCMPPS predicates; the purego generic
// treats NaN pairs as ordered instead).
//
// The AVX2 tier of the integer kernels tests "no lane greater" instead of
// "all lanes less-or-equal": integer LessEqual and unsigned comparisons only
// exist as EVEX encodings, while signed VPCMPGTD/VPCMPGTQ are available in
// AVX2; the unsigned variants bias both operands by the sign bit before
// comparing.

// Lane indexes of the shift-by-one ConcatPermute: lane i takes element i+1
// of the concatenation of the current and next chunks.
var (
	orderShift32 = [16]uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	orderShift64 = [8]uint64{1, 2, 3, 4, 5, 6, 7, 8}
)

func orderAscendingInt32(data []int32) bool {
	switch {
	case archsimd.X86.AVX512() && len(data) >= 17:
		idx := archsimd.LoadUint32x16Slice(orderShift32[:])
		chunks := unsafecast.Slice[[16]int32](data)
		cur := archsimd.LoadInt32x16Slice(chunks[0][:])
		for i := 1; i < len(chunks); i++ {
			nxt := archsimd.LoadInt32x16Slice(chunks[i][:])
			shifted := cur.ConcatPermute(nxt, idx)
			if cur.LessEqual(shifted).ToBits() != 0xffff {
				archsimd.ClearAVXUpperBits()
				return false
			}
			cur = nxt
		}
		// Pairs beginning in the last chunk or the remainder are covered
		// by one or two compares overlapping already checked elements.
		last := len(data) - 17
		a := archsimd.LoadInt32x16Slice(data[last:])
		b := archsimd.LoadInt32x16Slice(data[last+1:])
		if a.LessEqual(b).ToBits() != 0xffff {
			archsimd.ClearAVXUpperBits()
			return false
		}
		if s := (len(chunks) - 1) * 16; s < last {
			a = archsimd.LoadInt32x16Slice(data[s:])
			b = archsimd.LoadInt32x16Slice(data[s+1:])
			if a.LessEqual(b).ToBits() != 0xffff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	case archsimd.X86.AVX2() && len(data) >= 9:
		i := 0
		for ; i+9 <= len(data); i += 8 {
			a := archsimd.LoadInt32x8Slice(data[i:])
			b := archsimd.LoadInt32x8Slice(data[i+1:])
			if a.Greater(b).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		if i+1 < len(data) {
			a := archsimd.LoadInt32x8Slice(data[len(data)-9:])
			b := archsimd.LoadInt32x8Slice(data[len(data)-8:])
			if a.Greater(b).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	}
	for i := 0; i+1 < len(data); i++ {
		if data[i] > data[i+1] {
			return false
		}
	}
	return true
}

func orderDescendingInt32(data []int32) bool {
	switch {
	case archsimd.X86.AVX512() && len(data) >= 17:
		idx := archsimd.LoadUint32x16Slice(orderShift32[:])
		chunks := unsafecast.Slice[[16]int32](data)
		cur := archsimd.LoadInt32x16Slice(chunks[0][:])
		for i := 1; i < len(chunks); i++ {
			nxt := archsimd.LoadInt32x16Slice(chunks[i][:])
			shifted := cur.ConcatPermute(nxt, idx)
			if cur.GreaterEqual(shifted).ToBits() != 0xffff {
				archsimd.ClearAVXUpperBits()
				return false
			}
			cur = nxt
		}
		// Pairs beginning in the last chunk or the remainder are covered
		// by one or two compares overlapping already checked elements.
		last := len(data) - 17
		a := archsimd.LoadInt32x16Slice(data[last:])
		b := archsimd.LoadInt32x16Slice(data[last+1:])
		if a.GreaterEqual(b).ToBits() != 0xffff {
			archsimd.ClearAVXUpperBits()
			return false
		}
		if s := (len(chunks) - 1) * 16; s < last {
			a = archsimd.LoadInt32x16Slice(data[s:])
			b = archsimd.LoadInt32x16Slice(data[s+1:])
			if a.GreaterEqual(b).ToBits() != 0xffff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	case archsimd.X86.AVX2() && len(data) >= 9:
		i := 0
		for ; i+9 <= len(data); i += 8 {
			a := archsimd.LoadInt32x8Slice(data[i:])
			b := archsimd.LoadInt32x8Slice(data[i+1:])
			if b.Greater(a).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		if i+1 < len(data) {
			a := archsimd.LoadInt32x8Slice(data[len(data)-9:])
			b := archsimd.LoadInt32x8Slice(data[len(data)-8:])
			if b.Greater(a).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	}
	for i := 0; i+1 < len(data); i++ {
		if data[i] < data[i+1] {
			return false
		}
	}
	return true
}

func orderAscendingInt64(data []int64) bool {
	switch {
	case archsimd.X86.AVX512() && len(data) >= 9:
		idx := archsimd.LoadUint64x8Slice(orderShift64[:])
		chunks := unsafecast.Slice[[8]int64](data)
		cur := archsimd.LoadInt64x8Slice(chunks[0][:])
		for i := 1; i < len(chunks); i++ {
			nxt := archsimd.LoadInt64x8Slice(chunks[i][:])
			shifted := cur.ConcatPermute(nxt, idx)
			if cur.LessEqual(shifted).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
			cur = nxt
		}
		// Pairs beginning in the last chunk or the remainder are covered
		// by one or two compares overlapping already checked elements.
		last := len(data) - 9
		a := archsimd.LoadInt64x8Slice(data[last:])
		b := archsimd.LoadInt64x8Slice(data[last+1:])
		if a.LessEqual(b).ToBits() != 0xff {
			archsimd.ClearAVXUpperBits()
			return false
		}
		if s := (len(chunks) - 1) * 8; s < last {
			a = archsimd.LoadInt64x8Slice(data[s:])
			b = archsimd.LoadInt64x8Slice(data[s+1:])
			if a.LessEqual(b).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	case archsimd.X86.AVX2() && len(data) >= 5:
		i := 0
		for ; i+5 <= len(data); i += 4 {
			a := archsimd.LoadInt64x4Slice(data[i:])
			b := archsimd.LoadInt64x4Slice(data[i+1:])
			if a.Greater(b).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		if i+1 < len(data) {
			a := archsimd.LoadInt64x4Slice(data[len(data)-5:])
			b := archsimd.LoadInt64x4Slice(data[len(data)-4:])
			if a.Greater(b).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	}
	for i := 0; i+1 < len(data); i++ {
		if data[i] > data[i+1] {
			return false
		}
	}
	return true
}

func orderDescendingInt64(data []int64) bool {
	switch {
	case archsimd.X86.AVX512() && len(data) >= 9:
		idx := archsimd.LoadUint64x8Slice(orderShift64[:])
		chunks := unsafecast.Slice[[8]int64](data)
		cur := archsimd.LoadInt64x8Slice(chunks[0][:])
		for i := 1; i < len(chunks); i++ {
			nxt := archsimd.LoadInt64x8Slice(chunks[i][:])
			shifted := cur.ConcatPermute(nxt, idx)
			if cur.GreaterEqual(shifted).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
			cur = nxt
		}
		// Pairs beginning in the last chunk or the remainder are covered
		// by one or two compares overlapping already checked elements.
		last := len(data) - 9
		a := archsimd.LoadInt64x8Slice(data[last:])
		b := archsimd.LoadInt64x8Slice(data[last+1:])
		if a.GreaterEqual(b).ToBits() != 0xff {
			archsimd.ClearAVXUpperBits()
			return false
		}
		if s := (len(chunks) - 1) * 8; s < last {
			a = archsimd.LoadInt64x8Slice(data[s:])
			b = archsimd.LoadInt64x8Slice(data[s+1:])
			if a.GreaterEqual(b).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	case archsimd.X86.AVX2() && len(data) >= 5:
		i := 0
		for ; i+5 <= len(data); i += 4 {
			a := archsimd.LoadInt64x4Slice(data[i:])
			b := archsimd.LoadInt64x4Slice(data[i+1:])
			if b.Greater(a).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		if i+1 < len(data) {
			a := archsimd.LoadInt64x4Slice(data[len(data)-5:])
			b := archsimd.LoadInt64x4Slice(data[len(data)-4:])
			if b.Greater(a).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	}
	for i := 0; i+1 < len(data); i++ {
		if data[i] < data[i+1] {
			return false
		}
	}
	return true
}

func orderAscendingUint32(data []uint32) bool {
	switch {
	case archsimd.X86.AVX512() && len(data) >= 17:
		idx := archsimd.LoadUint32x16Slice(orderShift32[:])
		chunks := unsafecast.Slice[[16]uint32](data)
		cur := archsimd.LoadUint32x16Slice(chunks[0][:])
		for i := 1; i < len(chunks); i++ {
			nxt := archsimd.LoadUint32x16Slice(chunks[i][:])
			shifted := cur.ConcatPermute(nxt, idx)
			if cur.LessEqual(shifted).ToBits() != 0xffff {
				archsimd.ClearAVXUpperBits()
				return false
			}
			cur = nxt
		}
		// Pairs beginning in the last chunk or the remainder are covered
		// by one or two compares overlapping already checked elements.
		last := len(data) - 17
		a := archsimd.LoadUint32x16Slice(data[last:])
		b := archsimd.LoadUint32x16Slice(data[last+1:])
		if a.LessEqual(b).ToBits() != 0xffff {
			archsimd.ClearAVXUpperBits()
			return false
		}
		if s := (len(chunks) - 1) * 16; s < last {
			a = archsimd.LoadUint32x16Slice(data[s:])
			b = archsimd.LoadUint32x16Slice(data[s+1:])
			if a.LessEqual(b).ToBits() != 0xffff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	case archsimd.X86.AVX2() && len(data) >= 9:
		sign := archsimd.BroadcastUint32x8(1 << 31)
		i := 0
		for ; i+9 <= len(data); i += 8 {
			a := archsimd.LoadUint32x8Slice(data[i:])
			b := archsimd.LoadUint32x8Slice(data[i+1:])
			if a.Xor(sign).AsInt32x8().Greater(b.Xor(sign).AsInt32x8()).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		if i+1 < len(data) {
			a := archsimd.LoadUint32x8Slice(data[len(data)-9:])
			b := archsimd.LoadUint32x8Slice(data[len(data)-8:])
			if a.Xor(sign).AsInt32x8().Greater(b.Xor(sign).AsInt32x8()).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	}
	for i := 0; i+1 < len(data); i++ {
		if data[i] > data[i+1] {
			return false
		}
	}
	return true
}

func orderDescendingUint32(data []uint32) bool {
	switch {
	case archsimd.X86.AVX512() && len(data) >= 17:
		idx := archsimd.LoadUint32x16Slice(orderShift32[:])
		chunks := unsafecast.Slice[[16]uint32](data)
		cur := archsimd.LoadUint32x16Slice(chunks[0][:])
		for i := 1; i < len(chunks); i++ {
			nxt := archsimd.LoadUint32x16Slice(chunks[i][:])
			shifted := cur.ConcatPermute(nxt, idx)
			if cur.GreaterEqual(shifted).ToBits() != 0xffff {
				archsimd.ClearAVXUpperBits()
				return false
			}
			cur = nxt
		}
		// Pairs beginning in the last chunk or the remainder are covered
		// by one or two compares overlapping already checked elements.
		last := len(data) - 17
		a := archsimd.LoadUint32x16Slice(data[last:])
		b := archsimd.LoadUint32x16Slice(data[last+1:])
		if a.GreaterEqual(b).ToBits() != 0xffff {
			archsimd.ClearAVXUpperBits()
			return false
		}
		if s := (len(chunks) - 1) * 16; s < last {
			a = archsimd.LoadUint32x16Slice(data[s:])
			b = archsimd.LoadUint32x16Slice(data[s+1:])
			if a.GreaterEqual(b).ToBits() != 0xffff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	case archsimd.X86.AVX2() && len(data) >= 9:
		sign := archsimd.BroadcastUint32x8(1 << 31)
		i := 0
		for ; i+9 <= len(data); i += 8 {
			a := archsimd.LoadUint32x8Slice(data[i:])
			b := archsimd.LoadUint32x8Slice(data[i+1:])
			if b.Xor(sign).AsInt32x8().Greater(a.Xor(sign).AsInt32x8()).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		if i+1 < len(data) {
			a := archsimd.LoadUint32x8Slice(data[len(data)-9:])
			b := archsimd.LoadUint32x8Slice(data[len(data)-8:])
			if b.Xor(sign).AsInt32x8().Greater(a.Xor(sign).AsInt32x8()).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	}
	for i := 0; i+1 < len(data); i++ {
		if data[i] < data[i+1] {
			return false
		}
	}
	return true
}

func orderAscendingUint64(data []uint64) bool {
	switch {
	case archsimd.X86.AVX512() && len(data) >= 9:
		idx := archsimd.LoadUint64x8Slice(orderShift64[:])
		chunks := unsafecast.Slice[[8]uint64](data)
		cur := archsimd.LoadUint64x8Slice(chunks[0][:])
		for i := 1; i < len(chunks); i++ {
			nxt := archsimd.LoadUint64x8Slice(chunks[i][:])
			shifted := cur.ConcatPermute(nxt, idx)
			if cur.LessEqual(shifted).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
			cur = nxt
		}
		// Pairs beginning in the last chunk or the remainder are covered
		// by one or two compares overlapping already checked elements.
		last := len(data) - 9
		a := archsimd.LoadUint64x8Slice(data[last:])
		b := archsimd.LoadUint64x8Slice(data[last+1:])
		if a.LessEqual(b).ToBits() != 0xff {
			archsimd.ClearAVXUpperBits()
			return false
		}
		if s := (len(chunks) - 1) * 8; s < last {
			a = archsimd.LoadUint64x8Slice(data[s:])
			b = archsimd.LoadUint64x8Slice(data[s+1:])
			if a.LessEqual(b).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	case archsimd.X86.AVX2() && len(data) >= 5:
		sign := archsimd.BroadcastUint64x4(1 << 63)
		i := 0
		for ; i+5 <= len(data); i += 4 {
			a := archsimd.LoadUint64x4Slice(data[i:])
			b := archsimd.LoadUint64x4Slice(data[i+1:])
			if a.Xor(sign).AsInt64x4().Greater(b.Xor(sign).AsInt64x4()).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		if i+1 < len(data) {
			a := archsimd.LoadUint64x4Slice(data[len(data)-5:])
			b := archsimd.LoadUint64x4Slice(data[len(data)-4:])
			if a.Xor(sign).AsInt64x4().Greater(b.Xor(sign).AsInt64x4()).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	}
	for i := 0; i+1 < len(data); i++ {
		if data[i] > data[i+1] {
			return false
		}
	}
	return true
}

func orderDescendingUint64(data []uint64) bool {
	switch {
	case archsimd.X86.AVX512() && len(data) >= 9:
		idx := archsimd.LoadUint64x8Slice(orderShift64[:])
		chunks := unsafecast.Slice[[8]uint64](data)
		cur := archsimd.LoadUint64x8Slice(chunks[0][:])
		for i := 1; i < len(chunks); i++ {
			nxt := archsimd.LoadUint64x8Slice(chunks[i][:])
			shifted := cur.ConcatPermute(nxt, idx)
			if cur.GreaterEqual(shifted).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
			cur = nxt
		}
		// Pairs beginning in the last chunk or the remainder are covered
		// by one or two compares overlapping already checked elements.
		last := len(data) - 9
		a := archsimd.LoadUint64x8Slice(data[last:])
		b := archsimd.LoadUint64x8Slice(data[last+1:])
		if a.GreaterEqual(b).ToBits() != 0xff {
			archsimd.ClearAVXUpperBits()
			return false
		}
		if s := (len(chunks) - 1) * 8; s < last {
			a = archsimd.LoadUint64x8Slice(data[s:])
			b = archsimd.LoadUint64x8Slice(data[s+1:])
			if a.GreaterEqual(b).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	case archsimd.X86.AVX2() && len(data) >= 5:
		sign := archsimd.BroadcastUint64x4(1 << 63)
		i := 0
		for ; i+5 <= len(data); i += 4 {
			a := archsimd.LoadUint64x4Slice(data[i:])
			b := archsimd.LoadUint64x4Slice(data[i+1:])
			if b.Xor(sign).AsInt64x4().Greater(a.Xor(sign).AsInt64x4()).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		if i+1 < len(data) {
			a := archsimd.LoadUint64x4Slice(data[len(data)-5:])
			b := archsimd.LoadUint64x4Slice(data[len(data)-4:])
			if b.Xor(sign).AsInt64x4().Greater(a.Xor(sign).AsInt64x4()).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	}
	for i := 0; i+1 < len(data); i++ {
		if data[i] < data[i+1] {
			return false
		}
	}
	return true
}

func orderAscendingFloat32(data []float32) bool {
	switch {
	case archsimd.X86.AVX512() && len(data) >= 17:
		idx := archsimd.LoadUint32x16Slice(orderShift32[:])
		chunks := unsafecast.Slice[[16]float32](data)
		cur := archsimd.LoadFloat32x16Slice(chunks[0][:])
		for i := 1; i < len(chunks); i++ {
			nxt := archsimd.LoadFloat32x16Slice(chunks[i][:])
			shifted := cur.ConcatPermute(nxt, idx)
			if cur.LessEqual(shifted).ToBits() != 0xffff {
				archsimd.ClearAVXUpperBits()
				return false
			}
			cur = nxt
		}
		// Pairs beginning in the last chunk or the remainder are covered
		// by one or two compares overlapping already checked elements.
		last := len(data) - 17
		a := archsimd.LoadFloat32x16Slice(data[last:])
		b := archsimd.LoadFloat32x16Slice(data[last+1:])
		if a.LessEqual(b).ToBits() != 0xffff {
			archsimd.ClearAVXUpperBits()
			return false
		}
		if s := (len(chunks) - 1) * 16; s < last {
			a = archsimd.LoadFloat32x16Slice(data[s:])
			b = archsimd.LoadFloat32x16Slice(data[s+1:])
			if a.LessEqual(b).ToBits() != 0xffff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	case archsimd.X86.AVX2() && len(data) >= 9:
		i := 0
		for ; i+9 <= len(data); i += 8 {
			a := archsimd.LoadFloat32x8Slice(data[i:])
			b := archsimd.LoadFloat32x8Slice(data[i+1:])
			if a.LessEqual(b).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		if i+1 < len(data) {
			a := archsimd.LoadFloat32x8Slice(data[len(data)-9:])
			b := archsimd.LoadFloat32x8Slice(data[len(data)-8:])
			if a.LessEqual(b).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	}
	for i := 0; i+1 < len(data); i++ {
		if !(data[i] <= data[i+1]) {
			return false
		}
	}
	return true
}

func orderDescendingFloat32(data []float32) bool {
	switch {
	case archsimd.X86.AVX512() && len(data) >= 17:
		idx := archsimd.LoadUint32x16Slice(orderShift32[:])
		chunks := unsafecast.Slice[[16]float32](data)
		cur := archsimd.LoadFloat32x16Slice(chunks[0][:])
		for i := 1; i < len(chunks); i++ {
			nxt := archsimd.LoadFloat32x16Slice(chunks[i][:])
			shifted := cur.ConcatPermute(nxt, idx)
			if cur.GreaterEqual(shifted).ToBits() != 0xffff {
				archsimd.ClearAVXUpperBits()
				return false
			}
			cur = nxt
		}
		// Pairs beginning in the last chunk or the remainder are covered
		// by one or two compares overlapping already checked elements.
		last := len(data) - 17
		a := archsimd.LoadFloat32x16Slice(data[last:])
		b := archsimd.LoadFloat32x16Slice(data[last+1:])
		if a.GreaterEqual(b).ToBits() != 0xffff {
			archsimd.ClearAVXUpperBits()
			return false
		}
		if s := (len(chunks) - 1) * 16; s < last {
			a = archsimd.LoadFloat32x16Slice(data[s:])
			b = archsimd.LoadFloat32x16Slice(data[s+1:])
			if a.GreaterEqual(b).ToBits() != 0xffff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	case archsimd.X86.AVX2() && len(data) >= 9:
		i := 0
		for ; i+9 <= len(data); i += 8 {
			a := archsimd.LoadFloat32x8Slice(data[i:])
			b := archsimd.LoadFloat32x8Slice(data[i+1:])
			if a.GreaterEqual(b).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		if i+1 < len(data) {
			a := archsimd.LoadFloat32x8Slice(data[len(data)-9:])
			b := archsimd.LoadFloat32x8Slice(data[len(data)-8:])
			if a.GreaterEqual(b).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	}
	for i := 0; i+1 < len(data); i++ {
		if !(data[i] >= data[i+1]) {
			return false
		}
	}
	return true
}

func orderAscendingFloat64(data []float64) bool {
	switch {
	case archsimd.X86.AVX512() && len(data) >= 9:
		idx := archsimd.LoadUint64x8Slice(orderShift64[:])
		chunks := unsafecast.Slice[[8]float64](data)
		cur := archsimd.LoadFloat64x8Slice(chunks[0][:])
		for i := 1; i < len(chunks); i++ {
			nxt := archsimd.LoadFloat64x8Slice(chunks[i][:])
			shifted := cur.ConcatPermute(nxt, idx)
			if cur.LessEqual(shifted).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
			cur = nxt
		}
		// Pairs beginning in the last chunk or the remainder are covered
		// by one or two compares overlapping already checked elements.
		last := len(data) - 9
		a := archsimd.LoadFloat64x8Slice(data[last:])
		b := archsimd.LoadFloat64x8Slice(data[last+1:])
		if a.LessEqual(b).ToBits() != 0xff {
			archsimd.ClearAVXUpperBits()
			return false
		}
		if s := (len(chunks) - 1) * 8; s < last {
			a = archsimd.LoadFloat64x8Slice(data[s:])
			b = archsimd.LoadFloat64x8Slice(data[s+1:])
			if a.LessEqual(b).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	case archsimd.X86.AVX2() && len(data) >= 5:
		i := 0
		for ; i+5 <= len(data); i += 4 {
			a := archsimd.LoadFloat64x4Slice(data[i:])
			b := archsimd.LoadFloat64x4Slice(data[i+1:])
			if a.LessEqual(b).ToBits() != 0xf {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		if i+1 < len(data) {
			a := archsimd.LoadFloat64x4Slice(data[len(data)-5:])
			b := archsimd.LoadFloat64x4Slice(data[len(data)-4:])
			if a.LessEqual(b).ToBits() != 0xf {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	}
	for i := 0; i+1 < len(data); i++ {
		if !(data[i] <= data[i+1]) {
			return false
		}
	}
	return true
}

func orderDescendingFloat64(data []float64) bool {
	switch {
	case archsimd.X86.AVX512() && len(data) >= 9:
		idx := archsimd.LoadUint64x8Slice(orderShift64[:])
		chunks := unsafecast.Slice[[8]float64](data)
		cur := archsimd.LoadFloat64x8Slice(chunks[0][:])
		for i := 1; i < len(chunks); i++ {
			nxt := archsimd.LoadFloat64x8Slice(chunks[i][:])
			shifted := cur.ConcatPermute(nxt, idx)
			if cur.GreaterEqual(shifted).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
			cur = nxt
		}
		// Pairs beginning in the last chunk or the remainder are covered
		// by one or two compares overlapping already checked elements.
		last := len(data) - 9
		a := archsimd.LoadFloat64x8Slice(data[last:])
		b := archsimd.LoadFloat64x8Slice(data[last+1:])
		if a.GreaterEqual(b).ToBits() != 0xff {
			archsimd.ClearAVXUpperBits()
			return false
		}
		if s := (len(chunks) - 1) * 8; s < last {
			a = archsimd.LoadFloat64x8Slice(data[s:])
			b = archsimd.LoadFloat64x8Slice(data[s+1:])
			if a.GreaterEqual(b).ToBits() != 0xff {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	case archsimd.X86.AVX2() && len(data) >= 5:
		i := 0
		for ; i+5 <= len(data); i += 4 {
			a := archsimd.LoadFloat64x4Slice(data[i:])
			b := archsimd.LoadFloat64x4Slice(data[i+1:])
			if a.GreaterEqual(b).ToBits() != 0xf {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		if i+1 < len(data) {
			a := archsimd.LoadFloat64x4Slice(data[len(data)-5:])
			b := archsimd.LoadFloat64x4Slice(data[len(data)-4:])
			if a.GreaterEqual(b).ToBits() != 0xf {
				archsimd.ClearAVXUpperBits()
				return false
			}
		}
		archsimd.ClearAVXUpperBits()
		return true
	}
	for i := 0; i+1 < len(data); i++ {
		if !(data[i] >= data[i+1]) {
			return false
		}
	}
	return true
}

func orderOfInt32(data []int32) int {
	if len(data) > 1 {
		if orderAscendingInt32(data) {
			return +1
		}
		if orderDescendingInt32(data) {
			return -1
		}
	}
	return 0
}

func orderOfInt64(data []int64) int {
	if len(data) > 1 {
		if orderAscendingInt64(data) {
			return +1
		}
		if orderDescendingInt64(data) {
			return -1
		}
	}
	return 0
}

func orderOfUint32(data []uint32) int {
	if len(data) > 1 {
		if orderAscendingUint32(data) {
			return +1
		}
		if orderDescendingUint32(data) {
			return -1
		}
	}
	return 0
}

func orderOfUint64(data []uint64) int {
	if len(data) > 1 {
		if orderAscendingUint64(data) {
			return +1
		}
		if orderDescendingUint64(data) {
			return -1
		}
	}
	return 0
}

func orderOfFloat32(data []float32) int {
	if len(data) > 1 {
		if orderAscendingFloat32(data) {
			return +1
		}
		if orderDescendingFloat32(data) {
			return -1
		}
	}
	return 0
}

func orderOfFloat64(data []float64) int {
	if len(data) > 1 {
		if orderAscendingFloat64(data) {
			return +1
		}
		if orderDescendingFloat64(data) {
			return -1
		}
	}
	return 0
}
