//go:build goexperiment.simd

package parquet

import (
	"encoding/binary"
	"simd/archsimd"

	"github.com/parquet-go/bitpack/unsafecast"
)

// This file provides implementations of the page min/max/bounds kernels based
// on the simd/archsimd package, replacing the hand-written assembly of
// page_min_amd64.s, page_max_amd64.s and page_bounds_amd64.s when
// GOEXPERIMENT=simd is set. Unlike the assembly, which only had AVX-512
// paths, these implementations also provide AVX2 paths for CPUs without
// AVX-512.
//
// Floating point kernels ignore NaN values after the first element, matching
// the behavior of the assembly (floatPage.Bounds skips leading NaNs). The
// assembly guaranteed this with the operand order of VMINPS/VMAXPS, but
// archsimd's Min/Max do not document NaN semantics and the compiler may
// canonicalize the operands of commutative operations, so the float paths
// use an explicit compare-and-merge: Less/Greater are false for NaN, keeping
// the accumulator.
//
// The AVX2 tier of the 64-bit integer kernels also uses compare-and-merge:
// VPMINSQ/VPMAXSQ only exist in AVX-512, so Int64x4.Min would fault on
// AVX2-only CPUs. VPCMPGTQ is signed, so the unsigned variant biases both
// operands by 1<<63 before comparing.
//
// The vector paths never fall back to scalar code: the remainder is handled
// by reloading full vectors overlapping the already processed elements
// (min/max are idempotent), and the float reductions use in-register shuffle
// ladders. This matters for two reasons: scalar float compares emit legacy
// (non-VEX) UCOMISS/UCOMISD, which pay an AVX-SSE transition penalty after
// EVEX code, and the assembly avoided this with explicit VZEROUPPER, which
// Go code cannot express.

// The thresholds below are kept from the assembly implementation because
// page_bounds_amd64_test.go references them; the Go kernels use a single
// combined pass for bounds at every size.
const (
	combinedBoundsThreshold      = 1 * 1024 * 1024
	combinedBoundsInt64Threshold = (DefaultPageBufferSize*98/100 + 7) / 8
)

func reduceMinInt32x4(v archsimd.Int32x4) int32 {
	m := v.GetElem(0)
	if x := v.GetElem(1); x < m {
		m = x
	}
	if x := v.GetElem(2); x < m {
		m = x
	}
	if x := v.GetElem(3); x < m {
		m = x
	}
	return m
}

func reduceMaxInt32x4(v archsimd.Int32x4) int32 {
	m := v.GetElem(0)
	if x := v.GetElem(1); x > m {
		m = x
	}
	if x := v.GetElem(2); x > m {
		m = x
	}
	if x := v.GetElem(3); x > m {
		m = x
	}
	return m
}

func reduceMinInt64x2(v archsimd.Int64x2) int64 {
	m := v.GetElem(0)
	if x := v.GetElem(1); x < m {
		m = x
	}
	return m
}

func reduceMaxInt64x2(v archsimd.Int64x2) int64 {
	m := v.GetElem(0)
	if x := v.GetElem(1); x > m {
		m = x
	}
	return m
}

func reduceMinUint32x4(v archsimd.Uint32x4) uint32 {
	m := v.GetElem(0)
	if x := v.GetElem(1); x < m {
		m = x
	}
	if x := v.GetElem(2); x < m {
		m = x
	}
	if x := v.GetElem(3); x < m {
		m = x
	}
	return m
}

func reduceMaxUint32x4(v archsimd.Uint32x4) uint32 {
	m := v.GetElem(0)
	if x := v.GetElem(1); x > m {
		m = x
	}
	if x := v.GetElem(2); x > m {
		m = x
	}
	if x := v.GetElem(3); x > m {
		m = x
	}
	return m
}

func reduceMinUint64x2(v archsimd.Uint64x2) uint64 {
	m := v.GetElem(0)
	if x := v.GetElem(1); x < m {
		m = x
	}
	return m
}

func reduceMaxUint64x2(v archsimd.Uint64x2) uint64 {
	m := v.GetElem(0)
	if x := v.GetElem(1); x > m {
		m = x
	}
	return m
}

func reduceMinFloat32x4(v archsimd.Float32x4) float32 {
	p := v.SelectFromPair(2, 3, 0, 1, v)
	v = p.Merge(v, p.Less(v))
	p = v.SelectFromPair(1, 0, 3, 2, v)
	v = p.Merge(v, p.Less(v))
	return v.GetElem(0)
}

func reduceMaxFloat32x4(v archsimd.Float32x4) float32 {
	p := v.SelectFromPair(2, 3, 0, 1, v)
	v = p.Merge(v, p.Greater(v))
	p = v.SelectFromPair(1, 0, 3, 2, v)
	v = p.Merge(v, p.Greater(v))
	return v.GetElem(0)
}

func reduceMinFloat64x2(v archsimd.Float64x2) float64 {
	p := v.SelectFromPair(1, 0, v)
	v = p.Merge(v, p.Less(v))
	return v.GetElem(0)
}

func reduceMaxFloat64x2(v archsimd.Float64x2) float64 {
	p := v.SelectFromPair(1, 0, v)
	v = p.Merge(v, p.Greater(v))
	return v.GetElem(0)
}

func minInt32(data []int32) int32 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 32:
		acc0 := archsimd.BroadcastInt32x16(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[32]int32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadInt32x16Slice(c[0:16])
			v1 := archsimd.LoadInt32x16Slice(c[16:32])
			acc0 = acc0.Min(v0)
			acc1 = acc1.Min(v1)
		}
		if rem := len(d) - len(chunks)*32; rem > 0 {
			t0 := archsimd.LoadInt32x16Slice(d[len(d)-16:])
			acc0 = acc0.Min(t0)
			if rem > 16 {
				t1 := archsimd.LoadInt32x16Slice(d[len(d)-32:])
				acc1 = acc1.Min(t1)
			}
		}
		acc0 = acc0.Min(acc1)
		acc0H := acc0.GetLo().Min(acc0.GetHi())
		acc0Q := acc0H.GetLo().Min(acc0H.GetHi())
		acc0R := reduceMinInt32x4(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	case archsimd.X86.AVX2() && len(d) >= 16:
		acc0 := archsimd.BroadcastInt32x8(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[16]int32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadInt32x8Slice(c[0:8])
			v1 := archsimd.LoadInt32x8Slice(c[8:16])
			acc0 = acc0.Min(v0)
			acc1 = acc1.Min(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadInt32x8Slice(d[len(d)-8:])
			acc0 = acc0.Min(t0)
			if rem > 8 {
				t1 := archsimd.LoadInt32x8Slice(d[len(d)-16:])
				acc1 = acc1.Min(t1)
			}
		}
		acc0 = acc0.Min(acc1)
		acc0Q := acc0.GetLo().Min(acc0.GetHi())
		acc0R := reduceMinInt32x4(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	}
	for _, v := range d {
		if v < m {
			m = v
		}
	}
	return m
}

func maxInt32(data []int32) int32 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 32:
		acc0 := archsimd.BroadcastInt32x16(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[32]int32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadInt32x16Slice(c[0:16])
			v1 := archsimd.LoadInt32x16Slice(c[16:32])
			acc0 = acc0.Max(v0)
			acc1 = acc1.Max(v1)
		}
		if rem := len(d) - len(chunks)*32; rem > 0 {
			t0 := archsimd.LoadInt32x16Slice(d[len(d)-16:])
			acc0 = acc0.Max(t0)
			if rem > 16 {
				t1 := archsimd.LoadInt32x16Slice(d[len(d)-32:])
				acc1 = acc1.Max(t1)
			}
		}
		acc0 = acc0.Max(acc1)
		acc0H := acc0.GetLo().Max(acc0.GetHi())
		acc0Q := acc0H.GetLo().Max(acc0H.GetHi())
		acc0R := reduceMaxInt32x4(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	case archsimd.X86.AVX2() && len(d) >= 16:
		acc0 := archsimd.BroadcastInt32x8(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[16]int32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadInt32x8Slice(c[0:8])
			v1 := archsimd.LoadInt32x8Slice(c[8:16])
			acc0 = acc0.Max(v0)
			acc1 = acc1.Max(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadInt32x8Slice(d[len(d)-8:])
			acc0 = acc0.Max(t0)
			if rem > 8 {
				t1 := archsimd.LoadInt32x8Slice(d[len(d)-16:])
				acc1 = acc1.Max(t1)
			}
		}
		acc0 = acc0.Max(acc1)
		acc0Q := acc0.GetLo().Max(acc0.GetHi())
		acc0R := reduceMaxInt32x4(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	}
	for _, v := range d {
		if v > m {
			m = v
		}
	}
	return m
}

func minInt64(data []int64) int64 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 16:
		acc0 := archsimd.BroadcastInt64x8(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[16]int64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadInt64x8Slice(c[0:8])
			v1 := archsimd.LoadInt64x8Slice(c[8:16])
			acc0 = acc0.Min(v0)
			acc1 = acc1.Min(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadInt64x8Slice(d[len(d)-8:])
			acc0 = acc0.Min(t0)
			if rem > 8 {
				t1 := archsimd.LoadInt64x8Slice(d[len(d)-16:])
				acc1 = acc1.Min(t1)
			}
		}
		acc0 = acc0.Min(acc1)
		acc0H := acc0.GetLo().Min(acc0.GetHi())
		acc0Q := acc0H.GetLo().Min(acc0H.GetHi())
		acc0R := reduceMinInt64x2(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	case archsimd.X86.AVX2() && len(d) >= 8:
		acc0 := archsimd.BroadcastInt64x4(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[8]int64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadInt64x4Slice(c[0:4])
			v1 := archsimd.LoadInt64x4Slice(c[4:8])
			acc0 = v0.Merge(acc0, acc0.Greater(v0))
			acc1 = v1.Merge(acc1, acc1.Greater(v1))
		}
		if rem := len(d) - len(chunks)*8; rem > 0 {
			t0 := archsimd.LoadInt64x4Slice(d[len(d)-4:])
			acc0 = t0.Merge(acc0, acc0.Greater(t0))
			if rem > 4 {
				t1 := archsimd.LoadInt64x4Slice(d[len(d)-8:])
				acc1 = t1.Merge(acc1, acc1.Greater(t1))
			}
		}
		acc0 = acc1.Merge(acc0, acc0.Greater(acc1))
		acc0Q := acc0.GetHi().Merge(acc0.GetLo(), acc0.GetLo().Greater(acc0.GetHi()))
		acc0R := reduceMinInt64x2(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	}
	for _, v := range d {
		if v < m {
			m = v
		}
	}
	return m
}

func maxInt64(data []int64) int64 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 16:
		acc0 := archsimd.BroadcastInt64x8(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[16]int64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadInt64x8Slice(c[0:8])
			v1 := archsimd.LoadInt64x8Slice(c[8:16])
			acc0 = acc0.Max(v0)
			acc1 = acc1.Max(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadInt64x8Slice(d[len(d)-8:])
			acc0 = acc0.Max(t0)
			if rem > 8 {
				t1 := archsimd.LoadInt64x8Slice(d[len(d)-16:])
				acc1 = acc1.Max(t1)
			}
		}
		acc0 = acc0.Max(acc1)
		acc0H := acc0.GetLo().Max(acc0.GetHi())
		acc0Q := acc0H.GetLo().Max(acc0H.GetHi())
		acc0R := reduceMaxInt64x2(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	case archsimd.X86.AVX2() && len(d) >= 8:
		acc0 := archsimd.BroadcastInt64x4(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[8]int64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadInt64x4Slice(c[0:4])
			v1 := archsimd.LoadInt64x4Slice(c[4:8])
			acc0 = acc0.Merge(v0, acc0.Greater(v0))
			acc1 = acc1.Merge(v1, acc1.Greater(v1))
		}
		if rem := len(d) - len(chunks)*8; rem > 0 {
			t0 := archsimd.LoadInt64x4Slice(d[len(d)-4:])
			acc0 = acc0.Merge(t0, acc0.Greater(t0))
			if rem > 4 {
				t1 := archsimd.LoadInt64x4Slice(d[len(d)-8:])
				acc1 = acc1.Merge(t1, acc1.Greater(t1))
			}
		}
		acc0 = acc0.Merge(acc1, acc0.Greater(acc1))
		acc0Q := acc0.GetLo().Merge(acc0.GetHi(), acc0.GetLo().Greater(acc0.GetHi()))
		acc0R := reduceMaxInt64x2(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	}
	for _, v := range d {
		if v > m {
			m = v
		}
	}
	return m
}

func minUint32(data []uint32) uint32 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 32:
		acc0 := archsimd.BroadcastUint32x16(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[32]uint32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadUint32x16Slice(c[0:16])
			v1 := archsimd.LoadUint32x16Slice(c[16:32])
			acc0 = acc0.Min(v0)
			acc1 = acc1.Min(v1)
		}
		if rem := len(d) - len(chunks)*32; rem > 0 {
			t0 := archsimd.LoadUint32x16Slice(d[len(d)-16:])
			acc0 = acc0.Min(t0)
			if rem > 16 {
				t1 := archsimd.LoadUint32x16Slice(d[len(d)-32:])
				acc1 = acc1.Min(t1)
			}
		}
		acc0 = acc0.Min(acc1)
		acc0H := acc0.GetLo().Min(acc0.GetHi())
		acc0Q := acc0H.GetLo().Min(acc0H.GetHi())
		acc0R := reduceMinUint32x4(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	case archsimd.X86.AVX2() && len(d) >= 16:
		acc0 := archsimd.BroadcastUint32x8(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[16]uint32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadUint32x8Slice(c[0:8])
			v1 := archsimd.LoadUint32x8Slice(c[8:16])
			acc0 = acc0.Min(v0)
			acc1 = acc1.Min(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadUint32x8Slice(d[len(d)-8:])
			acc0 = acc0.Min(t0)
			if rem > 8 {
				t1 := archsimd.LoadUint32x8Slice(d[len(d)-16:])
				acc1 = acc1.Min(t1)
			}
		}
		acc0 = acc0.Min(acc1)
		acc0Q := acc0.GetLo().Min(acc0.GetHi())
		acc0R := reduceMinUint32x4(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	}
	for _, v := range d {
		if v < m {
			m = v
		}
	}
	return m
}

func maxUint32(data []uint32) uint32 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 32:
		acc0 := archsimd.BroadcastUint32x16(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[32]uint32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadUint32x16Slice(c[0:16])
			v1 := archsimd.LoadUint32x16Slice(c[16:32])
			acc0 = acc0.Max(v0)
			acc1 = acc1.Max(v1)
		}
		if rem := len(d) - len(chunks)*32; rem > 0 {
			t0 := archsimd.LoadUint32x16Slice(d[len(d)-16:])
			acc0 = acc0.Max(t0)
			if rem > 16 {
				t1 := archsimd.LoadUint32x16Slice(d[len(d)-32:])
				acc1 = acc1.Max(t1)
			}
		}
		acc0 = acc0.Max(acc1)
		acc0H := acc0.GetLo().Max(acc0.GetHi())
		acc0Q := acc0H.GetLo().Max(acc0H.GetHi())
		acc0R := reduceMaxUint32x4(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	case archsimd.X86.AVX2() && len(d) >= 16:
		acc0 := archsimd.BroadcastUint32x8(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[16]uint32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadUint32x8Slice(c[0:8])
			v1 := archsimd.LoadUint32x8Slice(c[8:16])
			acc0 = acc0.Max(v0)
			acc1 = acc1.Max(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadUint32x8Slice(d[len(d)-8:])
			acc0 = acc0.Max(t0)
			if rem > 8 {
				t1 := archsimd.LoadUint32x8Slice(d[len(d)-16:])
				acc1 = acc1.Max(t1)
			}
		}
		acc0 = acc0.Max(acc1)
		acc0Q := acc0.GetLo().Max(acc0.GetHi())
		acc0R := reduceMaxUint32x4(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	}
	for _, v := range d {
		if v > m {
			m = v
		}
	}
	return m
}

func minUint64(data []uint64) uint64 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 16:
		acc0 := archsimd.BroadcastUint64x8(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[16]uint64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadUint64x8Slice(c[0:8])
			v1 := archsimd.LoadUint64x8Slice(c[8:16])
			acc0 = acc0.Min(v0)
			acc1 = acc1.Min(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadUint64x8Slice(d[len(d)-8:])
			acc0 = acc0.Min(t0)
			if rem > 8 {
				t1 := archsimd.LoadUint64x8Slice(d[len(d)-16:])
				acc1 = acc1.Min(t1)
			}
		}
		acc0 = acc0.Min(acc1)
		acc0H := acc0.GetLo().Min(acc0.GetHi())
		acc0Q := acc0H.GetLo().Min(acc0H.GetHi())
		acc0R := reduceMinUint64x2(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	case archsimd.X86.AVX2() && len(d) >= 8:
		sign := archsimd.BroadcastUint64x4(1 << 63)
		sign2 := archsimd.BroadcastUint64x2(1 << 63)
		acc0 := archsimd.BroadcastUint64x4(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[8]uint64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadUint64x4Slice(c[0:4])
			v1 := archsimd.LoadUint64x4Slice(c[4:8])
			acc0 = v0.Merge(acc0, acc0.Xor(sign).AsInt64x4().Greater(v0.Xor(sign).AsInt64x4()))
			acc1 = v1.Merge(acc1, acc1.Xor(sign).AsInt64x4().Greater(v1.Xor(sign).AsInt64x4()))
		}
		if rem := len(d) - len(chunks)*8; rem > 0 {
			t0 := archsimd.LoadUint64x4Slice(d[len(d)-4:])
			acc0 = t0.Merge(acc0, acc0.Xor(sign).AsInt64x4().Greater(t0.Xor(sign).AsInt64x4()))
			if rem > 4 {
				t1 := archsimd.LoadUint64x4Slice(d[len(d)-8:])
				acc1 = t1.Merge(acc1, acc1.Xor(sign).AsInt64x4().Greater(t1.Xor(sign).AsInt64x4()))
			}
		}
		acc0 = acc1.Merge(acc0, acc0.Xor(sign).AsInt64x4().Greater(acc1.Xor(sign).AsInt64x4()))
		acc0Q := acc0.GetHi().Merge(acc0.GetLo(), acc0.GetLo().Xor(sign2).AsInt64x2().Greater(acc0.GetHi().Xor(sign2).AsInt64x2()))
		acc0R := reduceMinUint64x2(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	}
	for _, v := range d {
		if v < m {
			m = v
		}
	}
	return m
}

func maxUint64(data []uint64) uint64 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 16:
		acc0 := archsimd.BroadcastUint64x8(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[16]uint64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadUint64x8Slice(c[0:8])
			v1 := archsimd.LoadUint64x8Slice(c[8:16])
			acc0 = acc0.Max(v0)
			acc1 = acc1.Max(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadUint64x8Slice(d[len(d)-8:])
			acc0 = acc0.Max(t0)
			if rem > 8 {
				t1 := archsimd.LoadUint64x8Slice(d[len(d)-16:])
				acc1 = acc1.Max(t1)
			}
		}
		acc0 = acc0.Max(acc1)
		acc0H := acc0.GetLo().Max(acc0.GetHi())
		acc0Q := acc0H.GetLo().Max(acc0H.GetHi())
		acc0R := reduceMaxUint64x2(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	case archsimd.X86.AVX2() && len(d) >= 8:
		sign := archsimd.BroadcastUint64x4(1 << 63)
		sign2 := archsimd.BroadcastUint64x2(1 << 63)
		acc0 := archsimd.BroadcastUint64x4(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[8]uint64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadUint64x4Slice(c[0:4])
			v1 := archsimd.LoadUint64x4Slice(c[4:8])
			acc0 = acc0.Merge(v0, acc0.Xor(sign).AsInt64x4().Greater(v0.Xor(sign).AsInt64x4()))
			acc1 = acc1.Merge(v1, acc1.Xor(sign).AsInt64x4().Greater(v1.Xor(sign).AsInt64x4()))
		}
		if rem := len(d) - len(chunks)*8; rem > 0 {
			t0 := archsimd.LoadUint64x4Slice(d[len(d)-4:])
			acc0 = acc0.Merge(t0, acc0.Xor(sign).AsInt64x4().Greater(t0.Xor(sign).AsInt64x4()))
			if rem > 4 {
				t1 := archsimd.LoadUint64x4Slice(d[len(d)-8:])
				acc1 = acc1.Merge(t1, acc1.Xor(sign).AsInt64x4().Greater(t1.Xor(sign).AsInt64x4()))
			}
		}
		acc0 = acc0.Merge(acc1, acc0.Xor(sign).AsInt64x4().Greater(acc1.Xor(sign).AsInt64x4()))
		acc0Q := acc0.GetLo().Merge(acc0.GetHi(), acc0.GetLo().Xor(sign2).AsInt64x2().Greater(acc0.GetHi().Xor(sign2).AsInt64x2()))
		acc0R := reduceMaxUint64x2(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	}
	for _, v := range d {
		if v > m {
			m = v
		}
	}
	return m
}

func minFloat32(data []float32) float32 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 32:
		acc0 := archsimd.BroadcastFloat32x16(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[32]float32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat32x16Slice(c[0:16])
			v1 := archsimd.LoadFloat32x16Slice(c[16:32])
			acc0 = v0.Merge(acc0, v0.Less(acc0))
			acc1 = v1.Merge(acc1, v1.Less(acc1))
		}
		if rem := len(d) - len(chunks)*32; rem > 0 {
			t0 := archsimd.LoadFloat32x16Slice(d[len(d)-16:])
			acc0 = t0.Merge(acc0, t0.Less(acc0))
			if rem > 16 {
				t1 := archsimd.LoadFloat32x16Slice(d[len(d)-32:])
				acc1 = t1.Merge(acc1, t1.Less(acc1))
			}
		}
		acc0 = acc1.Merge(acc0, acc1.Less(acc0))
		acc0H := acc0.GetHi().Merge(acc0.GetLo(), acc0.GetHi().Less(acc0.GetLo()))
		acc0Q := acc0H.GetHi().Merge(acc0H.GetLo(), acc0H.GetHi().Less(acc0H.GetLo()))
		acc0R := reduceMinFloat32x4(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	case archsimd.X86.AVX2() && len(d) >= 16:
		acc0 := archsimd.BroadcastFloat32x8(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[16]float32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat32x8Slice(c[0:8])
			v1 := archsimd.LoadFloat32x8Slice(c[8:16])
			acc0 = v0.Merge(acc0, v0.Less(acc0))
			acc1 = v1.Merge(acc1, v1.Less(acc1))
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadFloat32x8Slice(d[len(d)-8:])
			acc0 = t0.Merge(acc0, t0.Less(acc0))
			if rem > 8 {
				t1 := archsimd.LoadFloat32x8Slice(d[len(d)-16:])
				acc1 = t1.Merge(acc1, t1.Less(acc1))
			}
		}
		acc0 = acc1.Merge(acc0, acc1.Less(acc0))
		acc0Q := acc0.GetHi().Merge(acc0.GetLo(), acc0.GetHi().Less(acc0.GetLo()))
		acc0R := reduceMinFloat32x4(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	}
	for _, v := range d {
		if v < m {
			m = v
		}
	}
	return m
}

func maxFloat32(data []float32) float32 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 32:
		acc0 := archsimd.BroadcastFloat32x16(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[32]float32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat32x16Slice(c[0:16])
			v1 := archsimd.LoadFloat32x16Slice(c[16:32])
			acc0 = v0.Merge(acc0, v0.Greater(acc0))
			acc1 = v1.Merge(acc1, v1.Greater(acc1))
		}
		if rem := len(d) - len(chunks)*32; rem > 0 {
			t0 := archsimd.LoadFloat32x16Slice(d[len(d)-16:])
			acc0 = t0.Merge(acc0, t0.Greater(acc0))
			if rem > 16 {
				t1 := archsimd.LoadFloat32x16Slice(d[len(d)-32:])
				acc1 = t1.Merge(acc1, t1.Greater(acc1))
			}
		}
		acc0 = acc1.Merge(acc0, acc1.Greater(acc0))
		acc0H := acc0.GetHi().Merge(acc0.GetLo(), acc0.GetHi().Greater(acc0.GetLo()))
		acc0Q := acc0H.GetHi().Merge(acc0H.GetLo(), acc0H.GetHi().Greater(acc0H.GetLo()))
		acc0R := reduceMaxFloat32x4(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	case archsimd.X86.AVX2() && len(d) >= 16:
		acc0 := archsimd.BroadcastFloat32x8(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[16]float32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat32x8Slice(c[0:8])
			v1 := archsimd.LoadFloat32x8Slice(c[8:16])
			acc0 = v0.Merge(acc0, v0.Greater(acc0))
			acc1 = v1.Merge(acc1, v1.Greater(acc1))
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadFloat32x8Slice(d[len(d)-8:])
			acc0 = t0.Merge(acc0, t0.Greater(acc0))
			if rem > 8 {
				t1 := archsimd.LoadFloat32x8Slice(d[len(d)-16:])
				acc1 = t1.Merge(acc1, t1.Greater(acc1))
			}
		}
		acc0 = acc1.Merge(acc0, acc1.Greater(acc0))
		acc0Q := acc0.GetHi().Merge(acc0.GetLo(), acc0.GetHi().Greater(acc0.GetLo()))
		acc0R := reduceMaxFloat32x4(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	}
	for _, v := range d {
		if v > m {
			m = v
		}
	}
	return m
}

func minFloat64(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 16:
		acc0 := archsimd.BroadcastFloat64x8(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[16]float64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat64x8Slice(c[0:8])
			v1 := archsimd.LoadFloat64x8Slice(c[8:16])
			acc0 = v0.Merge(acc0, v0.Less(acc0))
			acc1 = v1.Merge(acc1, v1.Less(acc1))
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadFloat64x8Slice(d[len(d)-8:])
			acc0 = t0.Merge(acc0, t0.Less(acc0))
			if rem > 8 {
				t1 := archsimd.LoadFloat64x8Slice(d[len(d)-16:])
				acc1 = t1.Merge(acc1, t1.Less(acc1))
			}
		}
		acc0 = acc1.Merge(acc0, acc1.Less(acc0))
		acc0H := acc0.GetHi().Merge(acc0.GetLo(), acc0.GetHi().Less(acc0.GetLo()))
		acc0Q := acc0H.GetHi().Merge(acc0H.GetLo(), acc0H.GetHi().Less(acc0H.GetLo()))
		acc0R := reduceMinFloat64x2(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	case archsimd.X86.AVX2() && len(d) >= 8:
		acc0 := archsimd.BroadcastFloat64x4(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[8]float64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat64x4Slice(c[0:4])
			v1 := archsimd.LoadFloat64x4Slice(c[4:8])
			acc0 = v0.Merge(acc0, v0.Less(acc0))
			acc1 = v1.Merge(acc1, v1.Less(acc1))
		}
		if rem := len(d) - len(chunks)*8; rem > 0 {
			t0 := archsimd.LoadFloat64x4Slice(d[len(d)-4:])
			acc0 = t0.Merge(acc0, t0.Less(acc0))
			if rem > 4 {
				t1 := archsimd.LoadFloat64x4Slice(d[len(d)-8:])
				acc1 = t1.Merge(acc1, t1.Less(acc1))
			}
		}
		acc0 = acc1.Merge(acc0, acc1.Less(acc0))
		acc0Q := acc0.GetHi().Merge(acc0.GetLo(), acc0.GetHi().Less(acc0.GetLo()))
		acc0R := reduceMinFloat64x2(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	}
	for _, v := range d {
		if v < m {
			m = v
		}
	}
	return m
}

func maxFloat64(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 16:
		acc0 := archsimd.BroadcastFloat64x8(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[16]float64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat64x8Slice(c[0:8])
			v1 := archsimd.LoadFloat64x8Slice(c[8:16])
			acc0 = v0.Merge(acc0, v0.Greater(acc0))
			acc1 = v1.Merge(acc1, v1.Greater(acc1))
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadFloat64x8Slice(d[len(d)-8:])
			acc0 = t0.Merge(acc0, t0.Greater(acc0))
			if rem > 8 {
				t1 := archsimd.LoadFloat64x8Slice(d[len(d)-16:])
				acc1 = t1.Merge(acc1, t1.Greater(acc1))
			}
		}
		acc0 = acc1.Merge(acc0, acc1.Greater(acc0))
		acc0H := acc0.GetHi().Merge(acc0.GetLo(), acc0.GetHi().Greater(acc0.GetLo()))
		acc0Q := acc0H.GetHi().Merge(acc0H.GetLo(), acc0H.GetHi().Greater(acc0H.GetLo()))
		acc0R := reduceMaxFloat64x2(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	case archsimd.X86.AVX2() && len(d) >= 8:
		acc0 := archsimd.BroadcastFloat64x4(m)
		acc1 := acc0
		chunks := unsafecast.Slice[[8]float64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat64x4Slice(c[0:4])
			v1 := archsimd.LoadFloat64x4Slice(c[4:8])
			acc0 = v0.Merge(acc0, v0.Greater(acc0))
			acc1 = v1.Merge(acc1, v1.Greater(acc1))
		}
		if rem := len(d) - len(chunks)*8; rem > 0 {
			t0 := archsimd.LoadFloat64x4Slice(d[len(d)-4:])
			acc0 = t0.Merge(acc0, t0.Greater(acc0))
			if rem > 4 {
				t1 := archsimd.LoadFloat64x4Slice(d[len(d)-8:])
				acc1 = t1.Merge(acc1, t1.Greater(acc1))
			}
		}
		acc0 = acc1.Merge(acc0, acc1.Greater(acc0))
		acc0Q := acc0.GetHi().Merge(acc0.GetLo(), acc0.GetHi().Greater(acc0.GetLo()))
		acc0R := reduceMaxFloat64x2(acc0Q)
		archsimd.ClearAVXUpperBits()
		return acc0R
	}
	for _, v := range d {
		if v > m {
			m = v
		}
	}
	return m
}

func boundsInt32(data []int32) (min, max int32) {
	if len(data) == 0 {
		return 0, 0
	}
	min = data[0]
	max = data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 32:
		minAcc0 := archsimd.BroadcastInt32x16(min)
		minAcc1 := minAcc0
		maxAcc0 := minAcc0
		maxAcc1 := minAcc0
		chunks := unsafecast.Slice[[32]int32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadInt32x16Slice(c[0:16])
			v1 := archsimd.LoadInt32x16Slice(c[16:32])
			minAcc0 = minAcc0.Min(v0)
			minAcc1 = minAcc1.Min(v1)
			maxAcc0 = maxAcc0.Max(v0)
			maxAcc1 = maxAcc1.Max(v1)
		}
		if rem := len(d) - len(chunks)*32; rem > 0 {
			t0 := archsimd.LoadInt32x16Slice(d[len(d)-16:])
			minAcc0 = minAcc0.Min(t0)
			maxAcc0 = maxAcc0.Max(t0)
			if rem > 16 {
				t1 := archsimd.LoadInt32x16Slice(d[len(d)-32:])
				minAcc1 = minAcc1.Min(t1)
				maxAcc1 = maxAcc1.Max(t1)
			}
		}
		minAcc0 = minAcc0.Min(minAcc1)
		maxAcc0 = maxAcc0.Max(maxAcc1)
		minAcc0H := minAcc0.GetLo().Min(minAcc0.GetHi())
		minAcc0Q := minAcc0H.GetLo().Min(minAcc0H.GetHi())
		minAcc0R := reduceMinInt32x4(minAcc0Q)
		maxAcc0H := maxAcc0.GetLo().Max(maxAcc0.GetHi())
		maxAcc0Q := maxAcc0H.GetLo().Max(maxAcc0H.GetHi())
		maxAcc0R := reduceMaxInt32x4(maxAcc0Q)
		archsimd.ClearAVXUpperBits()
		return minAcc0R, maxAcc0R
	case archsimd.X86.AVX2() && len(d) >= 16:
		minAcc0 := archsimd.BroadcastInt32x8(min)
		minAcc1 := minAcc0
		maxAcc0 := minAcc0
		maxAcc1 := minAcc0
		chunks := unsafecast.Slice[[16]int32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadInt32x8Slice(c[0:8])
			v1 := archsimd.LoadInt32x8Slice(c[8:16])
			minAcc0 = minAcc0.Min(v0)
			minAcc1 = minAcc1.Min(v1)
			maxAcc0 = maxAcc0.Max(v0)
			maxAcc1 = maxAcc1.Max(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadInt32x8Slice(d[len(d)-8:])
			minAcc0 = minAcc0.Min(t0)
			maxAcc0 = maxAcc0.Max(t0)
			if rem > 8 {
				t1 := archsimd.LoadInt32x8Slice(d[len(d)-16:])
				minAcc1 = minAcc1.Min(t1)
				maxAcc1 = maxAcc1.Max(t1)
			}
		}
		minAcc0 = minAcc0.Min(minAcc1)
		maxAcc0 = maxAcc0.Max(maxAcc1)
		minAcc0Q := minAcc0.GetLo().Min(minAcc0.GetHi())
		minAcc0R := reduceMinInt32x4(minAcc0Q)
		maxAcc0Q := maxAcc0.GetLo().Max(maxAcc0.GetHi())
		maxAcc0R := reduceMaxInt32x4(maxAcc0Q)
		archsimd.ClearAVXUpperBits()
		return minAcc0R, maxAcc0R
	}
	for _, v := range d {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}

func boundsInt64(data []int64) (min, max int64) {
	if len(data) == 0 {
		return 0, 0
	}
	min = data[0]
	max = data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 16:
		minAcc0 := archsimd.BroadcastInt64x8(min)
		minAcc1 := minAcc0
		maxAcc0 := minAcc0
		maxAcc1 := minAcc0
		chunks := unsafecast.Slice[[16]int64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadInt64x8Slice(c[0:8])
			v1 := archsimd.LoadInt64x8Slice(c[8:16])
			minAcc0 = minAcc0.Min(v0)
			minAcc1 = minAcc1.Min(v1)
			maxAcc0 = maxAcc0.Max(v0)
			maxAcc1 = maxAcc1.Max(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadInt64x8Slice(d[len(d)-8:])
			minAcc0 = minAcc0.Min(t0)
			maxAcc0 = maxAcc0.Max(t0)
			if rem > 8 {
				t1 := archsimd.LoadInt64x8Slice(d[len(d)-16:])
				minAcc1 = minAcc1.Min(t1)
				maxAcc1 = maxAcc1.Max(t1)
			}
		}
		minAcc0 = minAcc0.Min(minAcc1)
		maxAcc0 = maxAcc0.Max(maxAcc1)
		minAcc0H := minAcc0.GetLo().Min(minAcc0.GetHi())
		minAcc0Q := minAcc0H.GetLo().Min(minAcc0H.GetHi())
		minAcc0R := reduceMinInt64x2(minAcc0Q)
		maxAcc0H := maxAcc0.GetLo().Max(maxAcc0.GetHi())
		maxAcc0Q := maxAcc0H.GetLo().Max(maxAcc0H.GetHi())
		maxAcc0R := reduceMaxInt64x2(maxAcc0Q)
		archsimd.ClearAVXUpperBits()
		return minAcc0R, maxAcc0R
	case archsimd.X86.AVX2() && len(d) >= 8:
		minAcc0 := archsimd.BroadcastInt64x4(min)
		minAcc1 := minAcc0
		maxAcc0 := minAcc0
		maxAcc1 := minAcc0
		chunks := unsafecast.Slice[[8]int64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadInt64x4Slice(c[0:4])
			v1 := archsimd.LoadInt64x4Slice(c[4:8])
			minAcc0 = v0.Merge(minAcc0, minAcc0.Greater(v0))
			minAcc1 = v1.Merge(minAcc1, minAcc1.Greater(v1))
			maxAcc0 = maxAcc0.Merge(v0, maxAcc0.Greater(v0))
			maxAcc1 = maxAcc1.Merge(v1, maxAcc1.Greater(v1))
		}
		if rem := len(d) - len(chunks)*8; rem > 0 {
			t0 := archsimd.LoadInt64x4Slice(d[len(d)-4:])
			minAcc0 = t0.Merge(minAcc0, minAcc0.Greater(t0))
			maxAcc0 = maxAcc0.Merge(t0, maxAcc0.Greater(t0))
			if rem > 4 {
				t1 := archsimd.LoadInt64x4Slice(d[len(d)-8:])
				minAcc1 = t1.Merge(minAcc1, minAcc1.Greater(t1))
				maxAcc1 = maxAcc1.Merge(t1, maxAcc1.Greater(t1))
			}
		}
		minAcc0 = minAcc1.Merge(minAcc0, minAcc0.Greater(minAcc1))
		maxAcc0 = maxAcc0.Merge(maxAcc1, maxAcc0.Greater(maxAcc1))
		minAcc0Q := minAcc0.GetHi().Merge(minAcc0.GetLo(), minAcc0.GetLo().Greater(minAcc0.GetHi()))
		minAcc0R := reduceMinInt64x2(minAcc0Q)
		maxAcc0Q := maxAcc0.GetLo().Merge(maxAcc0.GetHi(), maxAcc0.GetLo().Greater(maxAcc0.GetHi()))
		maxAcc0R := reduceMaxInt64x2(maxAcc0Q)
		archsimd.ClearAVXUpperBits()
		return minAcc0R, maxAcc0R
	}
	for _, v := range d {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}

func boundsUint32(data []uint32) (min, max uint32) {
	if len(data) == 0 {
		return 0, 0
	}
	min = data[0]
	max = data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 32:
		minAcc0 := archsimd.BroadcastUint32x16(min)
		minAcc1 := minAcc0
		maxAcc0 := minAcc0
		maxAcc1 := minAcc0
		chunks := unsafecast.Slice[[32]uint32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadUint32x16Slice(c[0:16])
			v1 := archsimd.LoadUint32x16Slice(c[16:32])
			minAcc0 = minAcc0.Min(v0)
			minAcc1 = minAcc1.Min(v1)
			maxAcc0 = maxAcc0.Max(v0)
			maxAcc1 = maxAcc1.Max(v1)
		}
		if rem := len(d) - len(chunks)*32; rem > 0 {
			t0 := archsimd.LoadUint32x16Slice(d[len(d)-16:])
			minAcc0 = minAcc0.Min(t0)
			maxAcc0 = maxAcc0.Max(t0)
			if rem > 16 {
				t1 := archsimd.LoadUint32x16Slice(d[len(d)-32:])
				minAcc1 = minAcc1.Min(t1)
				maxAcc1 = maxAcc1.Max(t1)
			}
		}
		minAcc0 = minAcc0.Min(minAcc1)
		maxAcc0 = maxAcc0.Max(maxAcc1)
		minAcc0H := minAcc0.GetLo().Min(minAcc0.GetHi())
		minAcc0Q := minAcc0H.GetLo().Min(minAcc0H.GetHi())
		minAcc0R := reduceMinUint32x4(minAcc0Q)
		maxAcc0H := maxAcc0.GetLo().Max(maxAcc0.GetHi())
		maxAcc0Q := maxAcc0H.GetLo().Max(maxAcc0H.GetHi())
		maxAcc0R := reduceMaxUint32x4(maxAcc0Q)
		archsimd.ClearAVXUpperBits()
		return minAcc0R, maxAcc0R
	case archsimd.X86.AVX2() && len(d) >= 16:
		minAcc0 := archsimd.BroadcastUint32x8(min)
		minAcc1 := minAcc0
		maxAcc0 := minAcc0
		maxAcc1 := minAcc0
		chunks := unsafecast.Slice[[16]uint32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadUint32x8Slice(c[0:8])
			v1 := archsimd.LoadUint32x8Slice(c[8:16])
			minAcc0 = minAcc0.Min(v0)
			minAcc1 = minAcc1.Min(v1)
			maxAcc0 = maxAcc0.Max(v0)
			maxAcc1 = maxAcc1.Max(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadUint32x8Slice(d[len(d)-8:])
			minAcc0 = minAcc0.Min(t0)
			maxAcc0 = maxAcc0.Max(t0)
			if rem > 8 {
				t1 := archsimd.LoadUint32x8Slice(d[len(d)-16:])
				minAcc1 = minAcc1.Min(t1)
				maxAcc1 = maxAcc1.Max(t1)
			}
		}
		minAcc0 = minAcc0.Min(minAcc1)
		maxAcc0 = maxAcc0.Max(maxAcc1)
		minAcc0Q := minAcc0.GetLo().Min(minAcc0.GetHi())
		minAcc0R := reduceMinUint32x4(minAcc0Q)
		maxAcc0Q := maxAcc0.GetLo().Max(maxAcc0.GetHi())
		maxAcc0R := reduceMaxUint32x4(maxAcc0Q)
		archsimd.ClearAVXUpperBits()
		return minAcc0R, maxAcc0R
	}
	for _, v := range d {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}

func boundsUint64(data []uint64) (min, max uint64) {
	if len(data) == 0 {
		return 0, 0
	}
	min = data[0]
	max = data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 16:
		minAcc0 := archsimd.BroadcastUint64x8(min)
		minAcc1 := minAcc0
		maxAcc0 := minAcc0
		maxAcc1 := minAcc0
		chunks := unsafecast.Slice[[16]uint64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadUint64x8Slice(c[0:8])
			v1 := archsimd.LoadUint64x8Slice(c[8:16])
			minAcc0 = minAcc0.Min(v0)
			minAcc1 = minAcc1.Min(v1)
			maxAcc0 = maxAcc0.Max(v0)
			maxAcc1 = maxAcc1.Max(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadUint64x8Slice(d[len(d)-8:])
			minAcc0 = minAcc0.Min(t0)
			maxAcc0 = maxAcc0.Max(t0)
			if rem > 8 {
				t1 := archsimd.LoadUint64x8Slice(d[len(d)-16:])
				minAcc1 = minAcc1.Min(t1)
				maxAcc1 = maxAcc1.Max(t1)
			}
		}
		minAcc0 = minAcc0.Min(minAcc1)
		maxAcc0 = maxAcc0.Max(maxAcc1)
		minAcc0H := minAcc0.GetLo().Min(minAcc0.GetHi())
		minAcc0Q := minAcc0H.GetLo().Min(minAcc0H.GetHi())
		minAcc0R := reduceMinUint64x2(minAcc0Q)
		maxAcc0H := maxAcc0.GetLo().Max(maxAcc0.GetHi())
		maxAcc0Q := maxAcc0H.GetLo().Max(maxAcc0H.GetHi())
		maxAcc0R := reduceMaxUint64x2(maxAcc0Q)
		archsimd.ClearAVXUpperBits()
		return minAcc0R, maxAcc0R
	case archsimd.X86.AVX2() && len(d) >= 8:
		sign := archsimd.BroadcastUint64x4(1 << 63)
		sign2 := archsimd.BroadcastUint64x2(1 << 63)
		minAcc0 := archsimd.BroadcastUint64x4(min)
		minAcc1 := minAcc0
		maxAcc0 := minAcc0
		maxAcc1 := minAcc0
		chunks := unsafecast.Slice[[8]uint64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadUint64x4Slice(c[0:4])
			v1 := archsimd.LoadUint64x4Slice(c[4:8])
			minAcc0 = v0.Merge(minAcc0, minAcc0.Xor(sign).AsInt64x4().Greater(v0.Xor(sign).AsInt64x4()))
			minAcc1 = v1.Merge(minAcc1, minAcc1.Xor(sign).AsInt64x4().Greater(v1.Xor(sign).AsInt64x4()))
			maxAcc0 = maxAcc0.Merge(v0, maxAcc0.Xor(sign).AsInt64x4().Greater(v0.Xor(sign).AsInt64x4()))
			maxAcc1 = maxAcc1.Merge(v1, maxAcc1.Xor(sign).AsInt64x4().Greater(v1.Xor(sign).AsInt64x4()))
		}
		if rem := len(d) - len(chunks)*8; rem > 0 {
			t0 := archsimd.LoadUint64x4Slice(d[len(d)-4:])
			minAcc0 = t0.Merge(minAcc0, minAcc0.Xor(sign).AsInt64x4().Greater(t0.Xor(sign).AsInt64x4()))
			maxAcc0 = maxAcc0.Merge(t0, maxAcc0.Xor(sign).AsInt64x4().Greater(t0.Xor(sign).AsInt64x4()))
			if rem > 4 {
				t1 := archsimd.LoadUint64x4Slice(d[len(d)-8:])
				minAcc1 = t1.Merge(minAcc1, minAcc1.Xor(sign).AsInt64x4().Greater(t1.Xor(sign).AsInt64x4()))
				maxAcc1 = maxAcc1.Merge(t1, maxAcc1.Xor(sign).AsInt64x4().Greater(t1.Xor(sign).AsInt64x4()))
			}
		}
		minAcc0 = minAcc1.Merge(minAcc0, minAcc0.Xor(sign).AsInt64x4().Greater(minAcc1.Xor(sign).AsInt64x4()))
		maxAcc0 = maxAcc0.Merge(maxAcc1, maxAcc0.Xor(sign).AsInt64x4().Greater(maxAcc1.Xor(sign).AsInt64x4()))
		minAcc0Q := minAcc0.GetHi().Merge(minAcc0.GetLo(), minAcc0.GetLo().Xor(sign2).AsInt64x2().Greater(minAcc0.GetHi().Xor(sign2).AsInt64x2()))
		minAcc0R := reduceMinUint64x2(minAcc0Q)
		maxAcc0Q := maxAcc0.GetLo().Merge(maxAcc0.GetHi(), maxAcc0.GetLo().Xor(sign2).AsInt64x2().Greater(maxAcc0.GetHi().Xor(sign2).AsInt64x2()))
		maxAcc0R := reduceMaxUint64x2(maxAcc0Q)
		archsimd.ClearAVXUpperBits()
		return minAcc0R, maxAcc0R
	}
	for _, v := range d {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}

func boundsFloat32Merge(data []float32) (min, max float32) {
	if len(data) == 0 {
		return 0, 0
	}
	min = data[0]
	max = data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 32:
		minAcc0 := archsimd.BroadcastFloat32x16(min)
		minAcc1 := minAcc0
		maxAcc0 := minAcc0
		maxAcc1 := minAcc0
		chunks := unsafecast.Slice[[32]float32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat32x16Slice(c[0:16])
			v1 := archsimd.LoadFloat32x16Slice(c[16:32])
			minAcc0 = v0.Merge(minAcc0, v0.Less(minAcc0))
			minAcc1 = v1.Merge(minAcc1, v1.Less(minAcc1))
			maxAcc0 = v0.Merge(maxAcc0, v0.Greater(maxAcc0))
			maxAcc1 = v1.Merge(maxAcc1, v1.Greater(maxAcc1))
		}
		if rem := len(d) - len(chunks)*32; rem > 0 {
			t0 := archsimd.LoadFloat32x16Slice(d[len(d)-16:])
			minAcc0 = t0.Merge(minAcc0, t0.Less(minAcc0))
			maxAcc0 = t0.Merge(maxAcc0, t0.Greater(maxAcc0))
			if rem > 16 {
				t1 := archsimd.LoadFloat32x16Slice(d[len(d)-32:])
				minAcc1 = t1.Merge(minAcc1, t1.Less(minAcc1))
				maxAcc1 = t1.Merge(maxAcc1, t1.Greater(maxAcc1))
			}
		}
		minAcc0 = minAcc1.Merge(minAcc0, minAcc1.Less(minAcc0))
		maxAcc0 = maxAcc1.Merge(maxAcc0, maxAcc1.Greater(maxAcc0))
		minAcc0H := minAcc0.GetHi().Merge(minAcc0.GetLo(), minAcc0.GetHi().Less(minAcc0.GetLo()))
		minAcc0Q := minAcc0H.GetHi().Merge(minAcc0H.GetLo(), minAcc0H.GetHi().Less(minAcc0H.GetLo()))
		minAcc0R := reduceMinFloat32x4(minAcc0Q)
		maxAcc0H := maxAcc0.GetHi().Merge(maxAcc0.GetLo(), maxAcc0.GetHi().Greater(maxAcc0.GetLo()))
		maxAcc0Q := maxAcc0H.GetHi().Merge(maxAcc0H.GetLo(), maxAcc0H.GetHi().Greater(maxAcc0H.GetLo()))
		maxAcc0R := reduceMaxFloat32x4(maxAcc0Q)
		archsimd.ClearAVXUpperBits()
		return minAcc0R, maxAcc0R
	case archsimd.X86.AVX2() && len(d) >= 16:
		minAcc0 := archsimd.BroadcastFloat32x8(min)
		minAcc1 := minAcc0
		maxAcc0 := minAcc0
		maxAcc1 := minAcc0
		chunks := unsafecast.Slice[[16]float32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat32x8Slice(c[0:8])
			v1 := archsimd.LoadFloat32x8Slice(c[8:16])
			minAcc0 = v0.Merge(minAcc0, v0.Less(minAcc0))
			minAcc1 = v1.Merge(minAcc1, v1.Less(minAcc1))
			maxAcc0 = v0.Merge(maxAcc0, v0.Greater(maxAcc0))
			maxAcc1 = v1.Merge(maxAcc1, v1.Greater(maxAcc1))
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadFloat32x8Slice(d[len(d)-8:])
			minAcc0 = t0.Merge(minAcc0, t0.Less(minAcc0))
			maxAcc0 = t0.Merge(maxAcc0, t0.Greater(maxAcc0))
			if rem > 8 {
				t1 := archsimd.LoadFloat32x8Slice(d[len(d)-16:])
				minAcc1 = t1.Merge(minAcc1, t1.Less(minAcc1))
				maxAcc1 = t1.Merge(maxAcc1, t1.Greater(maxAcc1))
			}
		}
		minAcc0 = minAcc1.Merge(minAcc0, minAcc1.Less(minAcc0))
		maxAcc0 = maxAcc1.Merge(maxAcc0, maxAcc1.Greater(maxAcc0))
		minAcc0Q := minAcc0.GetHi().Merge(minAcc0.GetLo(), minAcc0.GetHi().Less(minAcc0.GetLo()))
		minAcc0R := reduceMinFloat32x4(minAcc0Q)
		maxAcc0Q := maxAcc0.GetHi().Merge(maxAcc0.GetLo(), maxAcc0.GetHi().Greater(maxAcc0.GetLo()))
		maxAcc0R := reduceMaxFloat32x4(maxAcc0Q)
		archsimd.ClearAVXUpperBits()
		return minAcc0R, maxAcc0R
	}
	for _, v := range d {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}

func boundsFloat64Merge(data []float64) (min, max float64) {
	if len(data) == 0 {
		return 0, 0
	}
	min = data[0]
	max = data[0]
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 16:
		minAcc0 := archsimd.BroadcastFloat64x8(min)
		minAcc1 := minAcc0
		maxAcc0 := minAcc0
		maxAcc1 := minAcc0
		chunks := unsafecast.Slice[[16]float64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat64x8Slice(c[0:8])
			v1 := archsimd.LoadFloat64x8Slice(c[8:16])
			minAcc0 = v0.Merge(minAcc0, v0.Less(minAcc0))
			minAcc1 = v1.Merge(minAcc1, v1.Less(minAcc1))
			maxAcc0 = v0.Merge(maxAcc0, v0.Greater(maxAcc0))
			maxAcc1 = v1.Merge(maxAcc1, v1.Greater(maxAcc1))
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadFloat64x8Slice(d[len(d)-8:])
			minAcc0 = t0.Merge(minAcc0, t0.Less(minAcc0))
			maxAcc0 = t0.Merge(maxAcc0, t0.Greater(maxAcc0))
			if rem > 8 {
				t1 := archsimd.LoadFloat64x8Slice(d[len(d)-16:])
				minAcc1 = t1.Merge(minAcc1, t1.Less(minAcc1))
				maxAcc1 = t1.Merge(maxAcc1, t1.Greater(maxAcc1))
			}
		}
		minAcc0 = minAcc1.Merge(minAcc0, minAcc1.Less(minAcc0))
		maxAcc0 = maxAcc1.Merge(maxAcc0, maxAcc1.Greater(maxAcc0))
		minAcc0H := minAcc0.GetHi().Merge(minAcc0.GetLo(), minAcc0.GetHi().Less(minAcc0.GetLo()))
		minAcc0Q := minAcc0H.GetHi().Merge(minAcc0H.GetLo(), minAcc0H.GetHi().Less(minAcc0H.GetLo()))
		minAcc0R := reduceMinFloat64x2(minAcc0Q)
		maxAcc0H := maxAcc0.GetHi().Merge(maxAcc0.GetLo(), maxAcc0.GetHi().Greater(maxAcc0.GetLo()))
		maxAcc0Q := maxAcc0H.GetHi().Merge(maxAcc0H.GetLo(), maxAcc0H.GetHi().Greater(maxAcc0H.GetLo()))
		maxAcc0R := reduceMaxFloat64x2(maxAcc0Q)
		archsimd.ClearAVXUpperBits()
		return minAcc0R, maxAcc0R
	case archsimd.X86.AVX2() && len(d) >= 8:
		minAcc0 := archsimd.BroadcastFloat64x4(min)
		minAcc1 := minAcc0
		maxAcc0 := minAcc0
		maxAcc1 := minAcc0
		chunks := unsafecast.Slice[[8]float64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat64x4Slice(c[0:4])
			v1 := archsimd.LoadFloat64x4Slice(c[4:8])
			minAcc0 = v0.Merge(minAcc0, v0.Less(minAcc0))
			minAcc1 = v1.Merge(minAcc1, v1.Less(minAcc1))
			maxAcc0 = v0.Merge(maxAcc0, v0.Greater(maxAcc0))
			maxAcc1 = v1.Merge(maxAcc1, v1.Greater(maxAcc1))
		}
		if rem := len(d) - len(chunks)*8; rem > 0 {
			t0 := archsimd.LoadFloat64x4Slice(d[len(d)-4:])
			minAcc0 = t0.Merge(minAcc0, t0.Less(minAcc0))
			maxAcc0 = t0.Merge(maxAcc0, t0.Greater(maxAcc0))
			if rem > 4 {
				t1 := archsimd.LoadFloat64x4Slice(d[len(d)-8:])
				minAcc1 = t1.Merge(minAcc1, t1.Less(minAcc1))
				maxAcc1 = t1.Merge(maxAcc1, t1.Greater(maxAcc1))
			}
		}
		minAcc0 = minAcc1.Merge(minAcc0, minAcc1.Less(minAcc0))
		maxAcc0 = maxAcc1.Merge(maxAcc0, maxAcc1.Greater(maxAcc0))
		minAcc0Q := minAcc0.GetHi().Merge(minAcc0.GetLo(), minAcc0.GetHi().Less(minAcc0.GetLo()))
		minAcc0R := reduceMinFloat64x2(minAcc0Q)
		maxAcc0Q := maxAcc0.GetHi().Merge(maxAcc0.GetLo(), maxAcc0.GetHi().Greater(maxAcc0.GetLo()))
		maxAcc0R := reduceMaxFloat64x2(maxAcc0Q)
		archsimd.ClearAVXUpperBits()
		return minAcc0R, maxAcc0R
	}
	for _, v := range d {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}

// The big endian 128 bits kernels compare 8 values per iteration: the
// values are loaded as pairs of uint64 lanes, byte swapped with a grouped
// byte shuffle (VPSHUFB, part of the AVX-512 feature bundle), and
// deinterleaved into high/low lanes; a lexicographic compare mask selects
// running minima together with their indexes. The final reduction stores
// the eight candidates once and scans them; the winning index recovers the
// pointer into the input.

var (
	bswap64x64 = [64]int8{
		7, 6, 5, 4, 3, 2, 1, 0, 15, 14, 13, 12, 11, 10, 9, 8,
		7, 6, 5, 4, 3, 2, 1, 0, 15, 14, 13, 12, 11, 10, 9, 8,
		7, 6, 5, 4, 3, 2, 1, 0, 15, 14, 13, 12, 11, 10, 9, 8,
		7, 6, 5, 4, 3, 2, 1, 0, 15, 14, 13, 12, 11, 10, 9, 8,
	}
	evenLanes64 = [8]uint64{0, 2, 4, 6, 8, 10, 12, 14}
	oddLanes64  = [8]uint64{1, 3, 5, 7, 9, 11, 13, 15}
	iotaLanes64 = [8]uint64{0, 1, 2, 3, 4, 5, 6, 7}
)

func minMaxBE128SIMD(data [][16]byte, max bool) int {
	bswap := archsimd.LoadInt8x64Slice(bswap64x64[:])
	even := archsimd.LoadUint64x8Slice(evenLanes64[:])
	odd := archsimd.LoadUint64x8Slice(oddLanes64[:])
	eight := archsimd.BroadcastUint64x8(8)

	m := len(data) / 8
	cv := unsafecast.Slice[[16]uint64](data)[:m]

	// Seed the running best from the first chunk: sentinel seeds would tie
	// with real extreme values and could report a wrong index.
	s0 := archsimd.LoadUint64x8Slice(cv[0][0:8]).AsUint8x64().PermuteOrZeroGrouped(bswap).AsUint64x8()
	s1 := archsimd.LoadUint64x8Slice(cv[0][8:16]).AsUint8x64().PermuteOrZeroGrouped(bswap).AsUint64x8()
	bestHi := s0.ConcatPermute(s1, even)
	bestLo := s0.ConcatPermute(s1, odd)
	bestIdx := archsimd.LoadUint64x8Slice(iotaLanes64[:])
	curIdx := bestIdx.Add(eight)

	for j := 1; j < m; j++ {
		z0 := archsimd.LoadUint64x8Slice(cv[j][0:8]).AsUint8x64().PermuteOrZeroGrouped(bswap).AsUint64x8()
		z1 := archsimd.LoadUint64x8Slice(cv[j][8:16]).AsUint8x64().PermuteOrZeroGrouped(bswap).AsUint64x8()
		hi := z0.ConcatPermute(z1, even)
		lo := z0.ConcatPermute(z1, odd)
		var better archsimd.Mask64x8
		if max {
			better = hi.Greater(bestHi).Or(hi.Equal(bestHi).And(lo.Greater(bestLo)))
		} else {
			better = hi.Less(bestHi).Or(hi.Equal(bestHi).And(lo.Less(bestLo)))
		}
		bestHi = hi.Merge(bestHi, better)
		bestLo = lo.Merge(bestLo, better)
		bestIdx = curIdx.Merge(bestIdx, better)
		curIdx = curIdx.Add(eight)
	}

	var his, los, idxs [8]uint64
	bestHi.StoreSlice(his[:])
	bestLo.StoreSlice(los[:])
	bestIdx.StoreSlice(idxs[:])
	archsimd.ClearAVXUpperBits()

	bh, bl, bi := his[0], los[0], int(idxs[0])
	for k := 1; k < 8; k++ {
		replace := false
		if max {
			replace = his[k] > bh || (his[k] == bh && los[k] > bl)
		} else {
			replace = his[k] < bh || (his[k] == bh && los[k] < bl)
		}
		// Prefer the earliest index on exact ties, matching the scalar scan.
		if !replace && his[k] == bh && los[k] == bl && int(idxs[k]) < bi {
			replace = true
		}
		if replace {
			bh, bl, bi = his[k], los[k], int(idxs[k])
		}
	}

	for k := m * 8; k < len(data); k++ {
		hi := binary.BigEndian.Uint64(data[k][:8])
		lo := binary.BigEndian.Uint64(data[k][8:])
		replace := false
		if max {
			replace = hi > bh || (hi == bh && lo > bl)
		} else {
			replace = hi < bh || (hi == bh && lo < bl)
		}
		if replace {
			bh, bl, bi = hi, lo, k
		}
	}
	return bi
}

func minBE128(data [][16]byte) (min []byte) {
	if len(data) == 0 {
		return nil
	}
	if archsimd.X86.AVX512() && len(data) >= 8 {
		return data[minMaxBE128SIMD(data, false)][:]
	}
	return minBE128Scalar(data)
}

func maxBE128(data [][16]byte) (max []byte) {
	if len(data) == 0 {
		return nil
	}
	if archsimd.X86.AVX512() && len(data) >= 8 {
		return data[minMaxBE128SIMD(data, true)][:]
	}
	return maxBE128Scalar(data)
}

func minBE128Scalar(data [][16]byte) (min []byte) {
	if len(data) > 0 {
		m := binary.BigEndian.Uint64(data[0][:8])
		j := 0
		for i := 1; i < len(data); i++ {
			x := binary.BigEndian.Uint64(data[i][:8])
			switch {
			case x < m:
				m, j = x, i
			case x == m:
				y := binary.BigEndian.Uint64(data[i][8:])
				n := binary.BigEndian.Uint64(data[j][8:])
				if y < n {
					m, j = x, i
				}
			}
		}
		min = data[j][:]
	}
	return min
}

func maxBE128Scalar(data [][16]byte) (max []byte) {
	if len(data) > 0 {
		m := binary.BigEndian.Uint64(data[0][:8])
		j := 0
		for i := 1; i < len(data); i++ {
			x := binary.BigEndian.Uint64(data[i][:8])
			switch {
			case x > m:
				m, j = x, i
			case x == m:
				y := binary.BigEndian.Uint64(data[i][8:])
				n := binary.BigEndian.Uint64(data[j][8:])
				if y > n {
					m, j = x, i
				}
			}
		}
		max = data[j][:]
	}
	return max
}

func boundsBE128(data [][16]byte) (min, max []byte) {
	min = minBE128(data)
	max = maxBE128(data)
	return min, max
}

// boundsFloat32 and boundsFloat64 optimistically scan with native Min/Max
// (one instruction per update instead of the NaN-safe compare-and-merge's
// two) while accumulating a sum of every loaded vector. Addition propagates
// NaN unconditionally — unlike VMINPS, which can silently erase one — so if
// the final sum has no NaN lane the data had no NaN and the fast result is
// exact; otherwise the compare-and-merge implementation rescans the data.
// A sum of +Inf and -Inf forces the rescan spuriously, costing time but
// never correctness. The native Min/Max may report the other sign of a
// tied +0/-0 bound than the merge implementation; the parquet float order
// treats them as equal.
func boundsFloat32(data []float32) (min, max float32) {
	if len(data) == 0 {
		return 0, 0
	}
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 64 && len(d) <= 8192:
		// Small inputs are compute bound and profit from four accumulator
		// sets (the clang generated kernels of viterin/vek and
		// kelindar/simd both run four); at cache resident sizes above the
		// threshold the two set loop below measured up to 33% faster, so
		// the wider unroll is fenced to small pages.
		minB0 := archsimd.BroadcastFloat32x16(data[0])
		minB1 := minB0
		minB2 := minB0
		minB3 := minB0
		maxB0 := minB0
		maxB1 := minB0
		maxB2 := minB0
		maxB3 := minB0
		sumB0 := archsimd.BroadcastUint32x16(0).AsFloat32x16()
		sumB1 := sumB0
		sumB2 := sumB0
		sumB3 := sumB0
		bchunks := unsafecast.Slice[[64]float32](d)
		for i := range bchunks {
			c := &bchunks[i]
			v0 := archsimd.LoadFloat32x16Slice(c[0:16])
			v1 := archsimd.LoadFloat32x16Slice(c[16:32])
			v2 := archsimd.LoadFloat32x16Slice(c[32:48])
			v3 := archsimd.LoadFloat32x16Slice(c[48:64])
			minB0 = minB0.Min(v0)
			minB1 = minB1.Min(v1)
			minB2 = minB2.Min(v2)
			minB3 = minB3.Min(v3)
			maxB0 = maxB0.Max(v0)
			maxB1 = maxB1.Max(v1)
			maxB2 = maxB2.Max(v2)
			maxB3 = maxB3.Max(v3)
			sumB0 = sumB0.Add(v0)
			sumB1 = sumB1.Add(v1)
			sumB2 = sumB2.Add(v2)
			sumB3 = sumB3.Add(v3)
		}
		if rem := len(d) - len(bchunks)*64; rem > 0 {
			t0 := archsimd.LoadFloat32x16Slice(d[len(d)-16:])
			minB0 = minB0.Min(t0)
			maxB0 = maxB0.Max(t0)
			sumB0 = sumB0.Add(t0)
			if rem > 16 {
				t1 := archsimd.LoadFloat32x16Slice(d[len(d)-32:])
				minB1 = minB1.Min(t1)
				maxB1 = maxB1.Max(t1)
				sumB1 = sumB1.Add(t1)
			}
			if rem > 32 {
				t2 := archsimd.LoadFloat32x16Slice(d[len(d)-48:])
				minB2 = minB2.Min(t2)
				maxB2 = maxB2.Max(t2)
				sumB2 = sumB2.Add(t2)
			}
			if rem > 48 {
				t3 := archsimd.LoadFloat32x16Slice(d[len(d)-64:])
				minB3 = minB3.Min(t3)
				maxB3 = maxB3.Max(t3)
				sumB3 = sumB3.Add(t3)
			}
		}
		if sumB0.Add(sumB1).Add(sumB2.Add(sumB3)).IsNaN().ToBits() != 0 {
			archsimd.ClearAVXUpperBits()
			return boundsFloat32Merge(data)
		}
		minB0 = minB0.Min(minB1).Min(minB2.Min(minB3))
		maxB0 = maxB0.Max(maxB1).Max(maxB2.Max(maxB3))
		minH := minB0.GetLo().Min(minB0.GetHi())
		maxH := maxB0.GetLo().Max(maxB0.GetHi())
		minQ := minH.GetLo().Min(minH.GetHi())
		maxQ := maxH.GetLo().Max(maxH.GetHi())
		min = reduceMinFloat32x4(minQ)
		max = reduceMaxFloat32x4(maxQ)
		archsimd.ClearAVXUpperBits()
		return min, max
	case archsimd.X86.AVX512() && len(d) >= 32:
		minA0 := archsimd.BroadcastFloat32x16(data[0])
		minA1 := minA0
		maxA0 := minA0
		maxA1 := minA0
		// The zero is built with an integer broadcast: a floating point
		// constant materializes through a legacy (non-VEX) XORPS, which
		// pays an AVX-SSE transition penalty inside EVEX code.
		sumA0 := archsimd.BroadcastUint32x16(0).AsFloat32x16()
		sumA1 := sumA0
		chunks := unsafecast.Slice[[32]float32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat32x16Slice(c[0:16])
			v1 := archsimd.LoadFloat32x16Slice(c[16:32])
			minA0 = minA0.Min(v0)
			minA1 = minA1.Min(v1)
			maxA0 = maxA0.Max(v0)
			maxA1 = maxA1.Max(v1)
			sumA0 = sumA0.Add(v0)
			sumA1 = sumA1.Add(v1)
		}
		if rem := len(d) - len(chunks)*32; rem > 0 {
			t0 := archsimd.LoadFloat32x16Slice(d[len(d)-16:])
			minA0 = minA0.Min(t0)
			maxA0 = maxA0.Max(t0)
			sumA0 = sumA0.Add(t0)
			if rem > 16 {
				t1 := archsimd.LoadFloat32x16Slice(d[len(d)-32:])
				minA1 = minA1.Min(t1)
				maxA1 = maxA1.Max(t1)
				sumA1 = sumA1.Add(t1)
			}
		}
		if sumA0.Add(sumA1).IsNaN().ToBits() != 0 {
			archsimd.ClearAVXUpperBits()
			return boundsFloat32Merge(data)
		}
		minA0 = minA0.Min(minA1)
		maxA0 = maxA0.Max(maxA1)
		minH := minA0.GetLo().Min(minA0.GetHi())
		maxH := maxA0.GetLo().Max(maxA0.GetHi())
		minQ := minH.GetLo().Min(minH.GetHi())
		maxQ := maxH.GetLo().Max(maxH.GetHi())
		min = reduceMinFloat32x4(minQ)
		max = reduceMaxFloat32x4(maxQ)
		archsimd.ClearAVXUpperBits()
		return min, max
	case archsimd.X86.AVX2() && len(d) >= 16:
		minA0 := archsimd.BroadcastFloat32x8(data[0])
		minA1 := minA0
		maxA0 := minA0
		maxA1 := minA0
		sumA0 := archsimd.BroadcastUint32x8(0).AsFloat32x8()
		sumA1 := sumA0
		chunks := unsafecast.Slice[[16]float32](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat32x8Slice(c[0:8])
			v1 := archsimd.LoadFloat32x8Slice(c[8:16])
			minA0 = minA0.Min(v0)
			minA1 = minA1.Min(v1)
			maxA0 = maxA0.Max(v0)
			maxA1 = maxA1.Max(v1)
			sumA0 = sumA0.Add(v0)
			sumA1 = sumA1.Add(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadFloat32x8Slice(d[len(d)-8:])
			minA0 = minA0.Min(t0)
			maxA0 = maxA0.Max(t0)
			sumA0 = sumA0.Add(t0)
			if rem > 8 {
				t1 := archsimd.LoadFloat32x8Slice(d[len(d)-16:])
				minA1 = minA1.Min(t1)
				maxA1 = maxA1.Max(t1)
				sumA1 = sumA1.Add(t1)
			}
		}
		if sumA0.Add(sumA1).IsNaN().ToBits() != 0 {
			archsimd.ClearAVXUpperBits()
			return boundsFloat32Merge(data)
		}
		minA0 = minA0.Min(minA1)
		maxA0 = maxA0.Max(maxA1)
		minQ := minA0.GetLo().Min(minA0.GetHi())
		maxQ := maxA0.GetLo().Max(maxA0.GetHi())
		min = reduceMinFloat32x4(minQ)
		max = reduceMaxFloat32x4(maxQ)
		archsimd.ClearAVXUpperBits()
		return min, max
	}
	return boundsFloat32Merge(data)
}

func boundsFloat64(data []float64) (min, max float64) {
	if len(data) == 0 {
		return 0, 0
	}
	d := data
	switch {
	case archsimd.X86.AVX512() && len(d) >= 32 && len(d) <= 4096:
		// Small inputs are compute bound and profit from four accumulator
		// sets (the clang generated kernels of viterin/vek and
		// kelindar/simd both run four); at cache resident sizes above the
		// threshold the two set loop below measured up to 33% faster, so
		// the wider unroll is fenced to small pages.
		minB0 := archsimd.BroadcastFloat64x8(data[0])
		minB1 := minB0
		minB2 := minB0
		minB3 := minB0
		maxB0 := minB0
		maxB1 := minB0
		maxB2 := minB0
		maxB3 := minB0
		sumB0 := archsimd.BroadcastUint64x8(0).AsFloat64x8()
		sumB1 := sumB0
		sumB2 := sumB0
		sumB3 := sumB0
		bchunks := unsafecast.Slice[[32]float64](d)
		for i := range bchunks {
			c := &bchunks[i]
			v0 := archsimd.LoadFloat64x8Slice(c[0:8])
			v1 := archsimd.LoadFloat64x8Slice(c[8:16])
			v2 := archsimd.LoadFloat64x8Slice(c[16:24])
			v3 := archsimd.LoadFloat64x8Slice(c[24:32])
			minB0 = minB0.Min(v0)
			minB1 = minB1.Min(v1)
			minB2 = minB2.Min(v2)
			minB3 = minB3.Min(v3)
			maxB0 = maxB0.Max(v0)
			maxB1 = maxB1.Max(v1)
			maxB2 = maxB2.Max(v2)
			maxB3 = maxB3.Max(v3)
			sumB0 = sumB0.Add(v0)
			sumB1 = sumB1.Add(v1)
			sumB2 = sumB2.Add(v2)
			sumB3 = sumB3.Add(v3)
		}
		if rem := len(d) - len(bchunks)*32; rem > 0 {
			t0 := archsimd.LoadFloat64x8Slice(d[len(d)-8:])
			minB0 = minB0.Min(t0)
			maxB0 = maxB0.Max(t0)
			sumB0 = sumB0.Add(t0)
			if rem > 8 {
				t1 := archsimd.LoadFloat64x8Slice(d[len(d)-16:])
				minB1 = minB1.Min(t1)
				maxB1 = maxB1.Max(t1)
				sumB1 = sumB1.Add(t1)
			}
			if rem > 16 {
				t2 := archsimd.LoadFloat64x8Slice(d[len(d)-24:])
				minB2 = minB2.Min(t2)
				maxB2 = maxB2.Max(t2)
				sumB2 = sumB2.Add(t2)
			}
			if rem > 24 {
				t3 := archsimd.LoadFloat64x8Slice(d[len(d)-32:])
				minB3 = minB3.Min(t3)
				maxB3 = maxB3.Max(t3)
				sumB3 = sumB3.Add(t3)
			}
		}
		if sumB0.Add(sumB1).Add(sumB2.Add(sumB3)).IsNaN().ToBits() != 0 {
			archsimd.ClearAVXUpperBits()
			return boundsFloat64Merge(data)
		}
		minB0 = minB0.Min(minB1).Min(minB2.Min(minB3))
		maxB0 = maxB0.Max(maxB1).Max(maxB2.Max(maxB3))
		minH := minB0.GetLo().Min(minB0.GetHi())
		maxH := maxB0.GetLo().Max(maxB0.GetHi())
		minQ := minH.GetLo().Min(minH.GetHi())
		maxQ := maxH.GetLo().Max(maxH.GetHi())
		min = reduceMinFloat64x2(minQ)
		max = reduceMaxFloat64x2(maxQ)
		archsimd.ClearAVXUpperBits()
		return min, max
	case archsimd.X86.AVX512() && len(d) >= 16:
		minA0 := archsimd.BroadcastFloat64x8(data[0])
		minA1 := minA0
		maxA0 := minA0
		maxA1 := minA0
		// The zero is built with an integer broadcast: a floating point
		// constant materializes through a legacy (non-VEX) XORPS, which
		// pays an AVX-SSE transition penalty inside EVEX code.
		sumA0 := archsimd.BroadcastUint64x8(0).AsFloat64x8()
		sumA1 := sumA0
		chunks := unsafecast.Slice[[16]float64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat64x8Slice(c[0:8])
			v1 := archsimd.LoadFloat64x8Slice(c[8:16])
			minA0 = minA0.Min(v0)
			minA1 = minA1.Min(v1)
			maxA0 = maxA0.Max(v0)
			maxA1 = maxA1.Max(v1)
			sumA0 = sumA0.Add(v0)
			sumA1 = sumA1.Add(v1)
		}
		if rem := len(d) - len(chunks)*16; rem > 0 {
			t0 := archsimd.LoadFloat64x8Slice(d[len(d)-8:])
			minA0 = minA0.Min(t0)
			maxA0 = maxA0.Max(t0)
			sumA0 = sumA0.Add(t0)
			if rem > 8 {
				t1 := archsimd.LoadFloat64x8Slice(d[len(d)-16:])
				minA1 = minA1.Min(t1)
				maxA1 = maxA1.Max(t1)
				sumA1 = sumA1.Add(t1)
			}
		}
		if sumA0.Add(sumA1).IsNaN().ToBits() != 0 {
			archsimd.ClearAVXUpperBits()
			return boundsFloat64Merge(data)
		}
		minA0 = minA0.Min(minA1)
		maxA0 = maxA0.Max(maxA1)
		minH := minA0.GetLo().Min(minA0.GetHi())
		maxH := maxA0.GetLo().Max(maxA0.GetHi())
		minQ := minH.GetLo().Min(minH.GetHi())
		maxQ := maxH.GetLo().Max(maxH.GetHi())
		min = reduceMinFloat64x2(minQ)
		max = reduceMaxFloat64x2(maxQ)
		archsimd.ClearAVXUpperBits()
		return min, max
	case archsimd.X86.AVX2() && len(d) >= 8:
		minA0 := archsimd.BroadcastFloat64x4(data[0])
		minA1 := minA0
		maxA0 := minA0
		maxA1 := minA0
		sumA0 := archsimd.BroadcastUint64x4(0).AsFloat64x4()
		sumA1 := sumA0
		chunks := unsafecast.Slice[[8]float64](d)
		for i := range chunks {
			c := &chunks[i]
			v0 := archsimd.LoadFloat64x4Slice(c[0:4])
			v1 := archsimd.LoadFloat64x4Slice(c[4:8])
			minA0 = minA0.Min(v0)
			minA1 = minA1.Min(v1)
			maxA0 = maxA0.Max(v0)
			maxA1 = maxA1.Max(v1)
			sumA0 = sumA0.Add(v0)
			sumA1 = sumA1.Add(v1)
		}
		if rem := len(d) - len(chunks)*8; rem > 0 {
			t0 := archsimd.LoadFloat64x4Slice(d[len(d)-4:])
			minA0 = minA0.Min(t0)
			maxA0 = maxA0.Max(t0)
			sumA0 = sumA0.Add(t0)
			if rem > 4 {
				t1 := archsimd.LoadFloat64x4Slice(d[len(d)-8:])
				minA1 = minA1.Min(t1)
				maxA1 = maxA1.Max(t1)
				sumA1 = sumA1.Add(t1)
			}
		}
		if sumA0.Add(sumA1).IsNaN().ToBits() != 0 {
			archsimd.ClearAVXUpperBits()
			return boundsFloat64Merge(data)
		}
		minA0 = minA0.Min(minA1)
		maxA0 = maxA0.Max(maxA1)
		minQ := minA0.GetLo().Min(minA0.GetHi())
		maxQ := maxA0.GetLo().Max(maxA0.GetHi())
		min = reduceMinFloat64x2(minQ)
		max = reduceMaxFloat64x2(maxQ)
		archsimd.ClearAVXUpperBits()
		return min, max
	}
	return boundsFloat64Merge(data)
}
