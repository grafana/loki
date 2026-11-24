//go:build goexperiment.simd

package bytestreamsplit

import (
	"encoding/binary"

	"simd/archsimd"
)

// This file provides implementations of the BYTE_STREAM_SPLIT codecs based
// on the simd/archsimd package, replacing the hand-written assembly of
// bytestreamsplit_amd64.s when GOEXPERIMENT=simd is set.
//
// The assembly used gather and scatter instructions, which archsimd does
// not expose; these implementations redesign the algorithm as a byte
// transpose in registers: VPERMB groups the bytes of each plane within a
// vector, and two-source permute trees (dword blocks for the 4 plane
// codecs, a 3 round qword butterfly for the 8 plane codecs) assemble whole
// plane vectors, with no gather or scatter at all. The vector paths require
// AVX512VBMI for VPERMB.
//
// The vector loops deliberately keep the computed-offset slicing below even
// though it carries 8 (float) or 16 (double) bounds checks per iteration:
// rewriting them as ranged chunk views (zero bounds checks, 30% fewer
// instructions) measured up to 65% SLOWER for the encoders at L2 sizes on
// Emerald Rapids. The plane stores all share the same 4KiB page offset
// (the plane stride is a multiple of 4096 for power of two inputs), and
// the leading hypothesis is that the tighter loop keeps more of those
// stores in flight per load, amplifying 4K load/store aliasing stalls the
// fatter loop naturally paced out (unprovable on GCP: the PMU is not
// virtualized). See the tier 4 notes in docs/archsimd-port.md.

var (
	bssEncGroup4 = [64]uint8{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 44, 48, 52, 56, 60,
		1, 5, 9, 13, 17, 21, 25, 29, 33, 37, 41, 45, 49, 53, 57, 61,
		2, 6, 10, 14, 18, 22, 26, 30, 34, 38, 42, 46, 50, 54, 58, 62,
		3, 7, 11, 15, 19, 23, 27, 31, 35, 39, 43, 47, 51, 55, 59, 63,
	}
	bssDecGroup4 = [64]uint8{
		0, 16, 32, 48, 1, 17, 33, 49, 2, 18, 34, 50, 3, 19, 35, 51,
		4, 20, 36, 52, 5, 21, 37, 53, 6, 22, 38, 54, 7, 23, 39, 55,
		8, 24, 40, 56, 9, 25, 41, 57, 10, 26, 42, 58, 11, 27, 43, 59,
		12, 28, 44, 60, 13, 29, 45, 61, 14, 30, 46, 62, 15, 31, 47, 63,
	}
	bssEncGroup8 = [64]uint8{
		0, 8, 16, 24, 32, 40, 48, 56,
		1, 9, 17, 25, 33, 41, 49, 57,
		2, 10, 18, 26, 34, 42, 50, 58,
		3, 11, 19, 27, 35, 43, 51, 59,
		4, 12, 20, 28, 36, 44, 52, 60,
		5, 13, 21, 29, 37, 45, 53, 61,
		6, 14, 22, 30, 38, 46, 54, 62,
		7, 15, 23, 31, 39, 47, 55, 63,
	}
	bssDecGroup8 = [64]uint8{
		0, 8, 16, 24, 32, 40, 48, 56,
		1, 9, 17, 25, 33, 41, 49, 57,
		2, 10, 18, 26, 34, 42, 50, 58,
		3, 11, 19, 27, 35, 43, 51, 59,
		4, 12, 20, 28, 36, 44, 52, 60,
		5, 13, 21, 29, 37, 45, 53, 61,
		6, 14, 22, 30, 38, 46, 54, 62,
		7, 15, 23, 31, 39, 47, 55, 63,
	}
	bssBlkLo4  = [16]uint32{0, 1, 2, 3, 16, 17, 18, 19, 4, 5, 6, 7, 20, 21, 22, 23}
	bssBlkHi4  = [16]uint32{8, 9, 10, 11, 24, 25, 26, 27, 12, 13, 14, 15, 28, 29, 30, 31}
	bssHalfLo4 = [16]uint32{0, 1, 2, 3, 4, 5, 6, 7, 16, 17, 18, 19, 20, 21, 22, 23}
	bssHalfHi4 = [16]uint32{8, 9, 10, 11, 12, 13, 14, 15, 24, 25, 26, 27, 28, 29, 30, 31}
	bssQLo1    = [8]uint64{0, 8, 1, 9, 2, 10, 3, 11}
	bssQHi1    = [8]uint64{4, 12, 5, 13, 6, 14, 7, 15}
	bssQLo2    = [8]uint64{0, 1, 8, 9, 2, 3, 10, 11}
	bssQHi2    = [8]uint64{4, 5, 12, 13, 6, 7, 14, 15}
	bssQLo3    = [8]uint64{0, 1, 2, 3, 8, 9, 10, 11}
	bssQHi3    = [8]uint64{4, 5, 6, 7, 12, 13, 14, 15}
)

func encodeFloat(dst, src []byte) {
	n := len(src) / 4
	i := 0
	if archsimd.X86.AVX512() && archsimd.X86.AVX512VBMI() && n >= 64 {
		g := archsimd.LoadUint8x64Slice(bssEncGroup4[:])
		blo := archsimd.LoadUint32x16Slice(bssBlkLo4[:])
		bhi := archsimd.LoadUint32x16Slice(bssBlkHi4[:])
		hlo := archsimd.LoadUint32x16Slice(bssHalfLo4[:])
		hhi := archsimd.LoadUint32x16Slice(bssHalfHi4[:])
		for ; i+64 <= n; i += 64 {
			y0 := archsimd.LoadUint8x64Slice(src[4*i+0 : 4*i+64]).Permute(g).AsUint32x16()
			y1 := archsimd.LoadUint8x64Slice(src[4*i+64 : 4*i+128]).Permute(g).AsUint32x16()
			y2 := archsimd.LoadUint8x64Slice(src[4*i+128 : 4*i+192]).Permute(g).AsUint32x16()
			y3 := archsimd.LoadUint8x64Slice(src[4*i+192 : 4*i+256]).Permute(g).AsUint32x16()
			t0 := y0.ConcatPermute(y1, blo)
			t1 := y0.ConcatPermute(y1, bhi)
			t2 := y2.ConcatPermute(y3, blo)
			t3 := y2.ConcatPermute(y3, bhi)
			t0.ConcatPermute(t2, hlo).AsUint8x64().StoreSlice(dst[0*n+i : 0*n+i+64])
			t0.ConcatPermute(t2, hhi).AsUint8x64().StoreSlice(dst[1*n+i : 1*n+i+64])
			t1.ConcatPermute(t3, hlo).AsUint8x64().StoreSlice(dst[2*n+i : 2*n+i+64])
			t1.ConcatPermute(t3, hhi).AsUint8x64().StoreSlice(dst[3*n+i : 3*n+i+64])
		}
		archsimd.ClearAVXUpperBits()
	}
	if i < n {
		b0 := dst[0*n : 1*n]
		b1 := dst[1*n : 2*n]
		b2 := dst[2*n : 3*n]
		b3 := dst[3*n : 4*n]
		for j := i; j < n; j++ {
			v := binary.LittleEndian.Uint32(src[4*j:])
			b0[j] = byte(v >> 0)
			b1[j] = byte(v >> 8)
			b2[j] = byte(v >> 16)
			b3[j] = byte(v >> 24)
		}
	}
}

func decodeFloat(dst, src []byte) {
	n := len(src) / 4
	i := 0
	if archsimd.X86.AVX512() && archsimd.X86.AVX512VBMI() && n >= 64 {
		g := archsimd.LoadUint8x64Slice(bssDecGroup4[:])
		blo := archsimd.LoadUint32x16Slice(bssBlkLo4[:])
		bhi := archsimd.LoadUint32x16Slice(bssBlkHi4[:])
		hlo := archsimd.LoadUint32x16Slice(bssHalfLo4[:])
		hhi := archsimd.LoadUint32x16Slice(bssHalfHi4[:])
		for ; i+64 <= n; i += 64 {
			p0 := archsimd.LoadUint8x64Slice(src[0*n+i : 0*n+i+64]).AsUint32x16()
			p1 := archsimd.LoadUint8x64Slice(src[1*n+i : 1*n+i+64]).AsUint32x16()
			p2 := archsimd.LoadUint8x64Slice(src[2*n+i : 2*n+i+64]).AsUint32x16()
			p3 := archsimd.LoadUint8x64Slice(src[3*n+i : 3*n+i+64]).AsUint32x16()
			t0 := p0.ConcatPermute(p1, blo)
			t1 := p0.ConcatPermute(p1, bhi)
			t2 := p2.ConcatPermute(p3, blo)
			t3 := p2.ConcatPermute(p3, bhi)
			t0.ConcatPermute(t2, hlo).AsUint8x64().Permute(g).StoreSlice(dst[4*i+0 : 4*i+64])
			t0.ConcatPermute(t2, hhi).AsUint8x64().Permute(g).StoreSlice(dst[4*i+64 : 4*i+128])
			t1.ConcatPermute(t3, hlo).AsUint8x64().Permute(g).StoreSlice(dst[4*i+128 : 4*i+192])
			t1.ConcatPermute(t3, hhi).AsUint8x64().Permute(g).StoreSlice(dst[4*i+192 : 4*i+256])
		}
		archsimd.ClearAVXUpperBits()
	}
	if i < n {
		b0 := src[0*n : 1*n]
		b1 := src[1*n : 2*n]
		b2 := src[2*n : 3*n]
		b3 := src[3*n : 4*n]
		for j := i; j < n; j++ {
			binary.LittleEndian.PutUint32(dst[4*j:], uint32(b0[j])|
				uint32(b1[j])<<8|
				uint32(b2[j])<<16|
				uint32(b3[j])<<24)
		}
	}
}

// transpose8x8Q transposes an 8x8 matrix of qwords held in 8 vectors.
func transpose8x8Q(y0, y1, y2, y3, y4, y5, y6, y7 archsimd.Uint64x8, lo1, hi1, lo2, hi2, lo3, hi3 archsimd.Uint64x8) (o0, o1, o2, o3, o4, o5, o6, o7 archsimd.Uint64x8) {
	a0 := y0.ConcatPermute(y1, lo1)
	a1 := y0.ConcatPermute(y1, hi1)
	a2 := y2.ConcatPermute(y3, lo1)
	a3 := y2.ConcatPermute(y3, hi1)
	a4 := y4.ConcatPermute(y5, lo1)
	a5 := y4.ConcatPermute(y5, hi1)
	a6 := y6.ConcatPermute(y7, lo1)
	a7 := y6.ConcatPermute(y7, hi1)
	b0 := a0.ConcatPermute(a2, lo2)
	b1 := a0.ConcatPermute(a2, hi2)
	b2 := a1.ConcatPermute(a3, lo2)
	b3 := a1.ConcatPermute(a3, hi2)
	b4 := a4.ConcatPermute(a6, lo2)
	b5 := a4.ConcatPermute(a6, hi2)
	b6 := a5.ConcatPermute(a7, lo2)
	b7 := a5.ConcatPermute(a7, hi2)
	o0 = b0.ConcatPermute(b4, lo3)
	o1 = b0.ConcatPermute(b4, hi3)
	o2 = b1.ConcatPermute(b5, lo3)
	o3 = b1.ConcatPermute(b5, hi3)
	o4 = b2.ConcatPermute(b6, lo3)
	o5 = b2.ConcatPermute(b6, hi3)
	o6 = b3.ConcatPermute(b7, lo3)
	o7 = b3.ConcatPermute(b7, hi3)
	return
}

func encodeDouble(dst, src []byte) {
	n := len(src) / 8
	i := 0
	if archsimd.X86.AVX512() && archsimd.X86.AVX512VBMI() && n >= 64 {
		g := archsimd.LoadUint8x64Slice(bssEncGroup8[:])
		lo1 := archsimd.LoadUint64x8Slice(bssQLo1[:])
		hi1 := archsimd.LoadUint64x8Slice(bssQHi1[:])
		lo2 := archsimd.LoadUint64x8Slice(bssQLo2[:])
		hi2 := archsimd.LoadUint64x8Slice(bssQHi2[:])
		lo3 := archsimd.LoadUint64x8Slice(bssQLo3[:])
		hi3 := archsimd.LoadUint64x8Slice(bssQHi3[:])
		for ; i+64 <= n; i += 64 {
			y0 := archsimd.LoadUint8x64Slice(src[8*i+0 : 8*i+64]).Permute(g).AsUint64x8()
			y1 := archsimd.LoadUint8x64Slice(src[8*i+64 : 8*i+128]).Permute(g).AsUint64x8()
			y2 := archsimd.LoadUint8x64Slice(src[8*i+128 : 8*i+192]).Permute(g).AsUint64x8()
			y3 := archsimd.LoadUint8x64Slice(src[8*i+192 : 8*i+256]).Permute(g).AsUint64x8()
			y4 := archsimd.LoadUint8x64Slice(src[8*i+256 : 8*i+320]).Permute(g).AsUint64x8()
			y5 := archsimd.LoadUint8x64Slice(src[8*i+320 : 8*i+384]).Permute(g).AsUint64x8()
			y6 := archsimd.LoadUint8x64Slice(src[8*i+384 : 8*i+448]).Permute(g).AsUint64x8()
			y7 := archsimd.LoadUint8x64Slice(src[8*i+448 : 8*i+512]).Permute(g).AsUint64x8()
			o0, o1, o2, o3, o4, o5, o6, o7 := transpose8x8Q(y0, y1, y2, y3, y4, y5, y6, y7, lo1, hi1, lo2, hi2, lo3, hi3)
			o0.AsUint8x64().StoreSlice(dst[0*n+i : 0*n+i+64])
			o1.AsUint8x64().StoreSlice(dst[1*n+i : 1*n+i+64])
			o2.AsUint8x64().StoreSlice(dst[2*n+i : 2*n+i+64])
			o3.AsUint8x64().StoreSlice(dst[3*n+i : 3*n+i+64])
			o4.AsUint8x64().StoreSlice(dst[4*n+i : 4*n+i+64])
			o5.AsUint8x64().StoreSlice(dst[5*n+i : 5*n+i+64])
			o6.AsUint8x64().StoreSlice(dst[6*n+i : 6*n+i+64])
			o7.AsUint8x64().StoreSlice(dst[7*n+i : 7*n+i+64])
		}
		archsimd.ClearAVXUpperBits()
	}
	if i < n {
		b0 := dst[0*n : 1*n]
		b1 := dst[1*n : 2*n]
		b2 := dst[2*n : 3*n]
		b3 := dst[3*n : 4*n]
		b4 := dst[4*n : 5*n]
		b5 := dst[5*n : 6*n]
		b6 := dst[6*n : 7*n]
		b7 := dst[7*n : 8*n]
		for j := i; j < n; j++ {
			v := binary.LittleEndian.Uint64(src[8*j:])
			b0[j] = byte(v >> 0)
			b1[j] = byte(v >> 8)
			b2[j] = byte(v >> 16)
			b3[j] = byte(v >> 24)
			b4[j] = byte(v >> 32)
			b5[j] = byte(v >> 40)
			b6[j] = byte(v >> 48)
			b7[j] = byte(v >> 56)
		}
	}
}

func decodeDouble(dst, src []byte) {
	n := len(src) / 8
	i := 0
	if archsimd.X86.AVX512() && archsimd.X86.AVX512VBMI() && n >= 64 {
		g := archsimd.LoadUint8x64Slice(bssDecGroup8[:])
		lo1 := archsimd.LoadUint64x8Slice(bssQLo1[:])
		hi1 := archsimd.LoadUint64x8Slice(bssQHi1[:])
		lo2 := archsimd.LoadUint64x8Slice(bssQLo2[:])
		hi2 := archsimd.LoadUint64x8Slice(bssQHi2[:])
		lo3 := archsimd.LoadUint64x8Slice(bssQLo3[:])
		hi3 := archsimd.LoadUint64x8Slice(bssQHi3[:])
		for ; i+64 <= n; i += 64 {
			p0 := archsimd.LoadUint8x64Slice(src[0*n+i : 0*n+i+64]).AsUint64x8()
			p1 := archsimd.LoadUint8x64Slice(src[1*n+i : 1*n+i+64]).AsUint64x8()
			p2 := archsimd.LoadUint8x64Slice(src[2*n+i : 2*n+i+64]).AsUint64x8()
			p3 := archsimd.LoadUint8x64Slice(src[3*n+i : 3*n+i+64]).AsUint64x8()
			p4 := archsimd.LoadUint8x64Slice(src[4*n+i : 4*n+i+64]).AsUint64x8()
			p5 := archsimd.LoadUint8x64Slice(src[5*n+i : 5*n+i+64]).AsUint64x8()
			p6 := archsimd.LoadUint8x64Slice(src[6*n+i : 6*n+i+64]).AsUint64x8()
			p7 := archsimd.LoadUint8x64Slice(src[7*n+i : 7*n+i+64]).AsUint64x8()
			o0, o1, o2, o3, o4, o5, o6, o7 := transpose8x8Q(p0, p1, p2, p3, p4, p5, p6, p7, lo1, hi1, lo2, hi2, lo3, hi3)
			o0.AsUint8x64().Permute(g).StoreSlice(dst[8*i+0 : 8*i+64])
			o1.AsUint8x64().Permute(g).StoreSlice(dst[8*i+64 : 8*i+128])
			o2.AsUint8x64().Permute(g).StoreSlice(dst[8*i+128 : 8*i+192])
			o3.AsUint8x64().Permute(g).StoreSlice(dst[8*i+192 : 8*i+256])
			o4.AsUint8x64().Permute(g).StoreSlice(dst[8*i+256 : 8*i+320])
			o5.AsUint8x64().Permute(g).StoreSlice(dst[8*i+320 : 8*i+384])
			o6.AsUint8x64().Permute(g).StoreSlice(dst[8*i+384 : 8*i+448])
			o7.AsUint8x64().Permute(g).StoreSlice(dst[8*i+448 : 8*i+512])
		}
		archsimd.ClearAVXUpperBits()
	}
	if i < n {
		b0 := src[0*n : 1*n]
		b1 := src[1*n : 2*n]
		b2 := src[2*n : 3*n]
		b3 := src[3*n : 4*n]
		b4 := src[4*n : 5*n]
		b5 := src[5*n : 6*n]
		b6 := src[6*n : 7*n]
		b7 := src[7*n : 8*n]
		for j := i; j < n; j++ {
			binary.LittleEndian.PutUint64(dst[8*j:], uint64(b0[j])|
				uint64(b1[j])<<8|
				uint64(b2[j])<<16|
				uint64(b3[j])<<24|
				uint64(b4[j])<<32|
				uint64(b5[j])<<40|
				uint64(b6[j])<<48|
				uint64(b7[j])<<56)
		}
	}
}

func encodeInt32(dst, src []byte) {
	encodeFloat(dst, src)
}

func decodeInt32(dst, src []byte) {
	decodeFloat(dst, src)
}

func encodeInt64(dst, src []byte) {
	encodeDouble(dst, src)
}

func decodeInt64(dst, src []byte) {
	decodeDouble(dst, src)
}
