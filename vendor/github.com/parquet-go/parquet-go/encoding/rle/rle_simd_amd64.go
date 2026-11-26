//go:build goexperiment.simd

package rle

import (
	"encoding/binary"

	"simd/archsimd"

	"github.com/parquet-go/bitpack/unsafecast"
)

// The functions in this file are simd/archsimd replacements for some of the
// kernels dispatched through the function variables of rle_amd64.go (assembly
// builds) or rle_purego.go (purego builds). The BMI2 bit-packing kernels have
// no archsimd equivalent (PDEP/PEXT are not exposed) and keep their default
// dispatch.
//
// The override is safe in both builds: package variable initializers run
// before all init functions, and Go compiles the files of a package in file
// name order, so this init runs after the one in rle_amd64.go.
func init() {
	if archsimd.X86.AVX2() {
		encodeInt32IndexEqual8Contiguous = encodeInt32IndexEqual8ContiguousSIMD
	}
	if archsimd.X86.AVX512() {
		encodeInt32Bitpack = encodeInt32BitpackSIMD
	}
	if archsimd.X86.AVX512() && archsimd.X86.AVX512VBMI() {
		encodeBytesBitpack = encodeBytesBitpackSIMD
		decodeBytesBitpack = decodeBytesBitpackSIMD
	}
}

// rotate1x8 rotates the 8 lanes of a vector left by one position; a group is
// uniform if and only if it equals its own rotation.
var rotate1x8 = [8]uint32{1, 2, 3, 4, 5, 6, 7, 0}

// encodeInt32IndexEqual8ContiguousSIMD returns the index of the first group of
// 8 words that all have the same value.
//
// Comparing each group against its own lane rotation avoids the
// memory-to-GPR-to-vector round trip of broadcasting the first element, and
// the loop is unrolled 4 groups per iteration to amortize the loop overhead;
// checking the group masks in order preserves the first-match semantics.
func encodeInt32IndexEqual8ContiguousSIMD(words [][8]int32) (n int) {
	rot := archsimd.LoadUint32x8Slice(rotate1x8[:])
	// The array pointer conversion carries the only bounds check of the loop
	// body; the constant group indexes below compile to constant address
	// offsets with no checks.
	d := words
	for len(d) >= 4 {
		c := (*[4][8]int32)(d)
		w0 := archsimd.LoadInt32x8Slice(c[0][:])
		w1 := archsimd.LoadInt32x8Slice(c[1][:])
		w2 := archsimd.LoadInt32x8Slice(c[2][:])
		w3 := archsimd.LoadInt32x8Slice(c[3][:])
		e0 := w0.Equal(w0.Permute(rot)).ToBits()
		e1 := w1.Equal(w1.Permute(rot)).ToBits()
		e2 := w2.Equal(w2.Permute(rot)).ToBits()
		e3 := w3.Equal(w3.Permute(rot)).ToBits()
		// e+1 carries into bit 8 only when e == 0xFF, so this tests whether
		// any of the four groups is uniform with a single branch.
		any := (uint32(e0) + 1) | (uint32(e1) + 1) | (uint32(e2) + 1) | (uint32(e3) + 1)
		if any&0x100 == 0 {
			d = d[4:]
			continue
		}
		archsimd.ClearAVXUpperBits()
		n = len(words) - len(d)
		switch {
		case e0 == 0xFF:
		case e1 == 0xFF:
			n++
		case e2 == 0xFF:
			n += 2
		default:
			n += 3
		}
		return n
	}
	for n = len(words) - len(d); n < len(words); n++ {
		w := archsimd.LoadInt32x8Slice(words[n][:])
		if w.Equal(w.Permute(rot)).ToBits() == 0xFF {
			break
		}
	}
	archsimd.ClearAVXUpperBits()
	return n
}

// Lane indexes folding odd lanes onto even lanes and lanes {2,6} onto {0,4}
// in the packing reduction (same shape as the DELTA_BINARY_PACKED mini block
// packer; the useful results live in lanes 0 and 4 afterwards).
var (
	bitpackFoldOdd  = [8]uint64{1, 1, 3, 3, 5, 5, 7, 7}
	bitpackFoldPair = [8]uint64{2, 2, 2, 2, 6, 6, 6, 6}
)

func encodeInt32BitpackSIMD(dst []byte, src [][8]int32, bitWidth uint) int {
	switch {
	case bitWidth == 0:
		return 0
	case bitWidth <= 16:
		return encodeInt32Bitpack1to16bitsSIMD(dst, src, bitWidth)
	default:
		return encodeInt32BitpackDefault(dst, src, bitWidth)
	}
}

// encodeInt32Bitpack1to16bitsSIMD packs groups of 8 values of 1 to 16 bits
// each with the fold reduction used by the DELTA_BINARY_PACKED mini block
// packers: two vector fold steps leave 4 packed values in lane 0 and 4 in
// lane 4, and a scalar 128 bits stitch combines them. Each group spans
// exactly bitWidth bytes and stores 16 bytes at its byte aligned offset; the
// zeroed tail is overwritten by the next group and the caller reserves 32
// bytes of headroom (appendBitPackedInt32), like it did for the assembly.
func encodeInt32Bitpack1to16bitsSIMD(dst []byte, src [][8]int32, bitWidth uint) int {
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
	fo := archsimd.LoadUint64x8Slice(bitpackFoldOdd[:])
	fp := archsimd.LoadUint64x8Slice(bitpackFoldPair[:])
	off := uint(0)
	j := 0
	for ; j+2 <= len(src); j += 2 {
		u0 := unsafecast.Slice[uint32](src[j][:])
		u1 := unsafecast.Slice[uint32](src[j+1][:])
		t0 := archsimd.LoadUint32x8Slice(u0).ExtendToUint64().ShiftLeft(sh1)
		t1 := archsimd.LoadUint32x8Slice(u1).ExtendToUint64().ShiftLeft(sh1)
		t0 = t0.Or(t0.Permute(fo))
		t1 = t1.Or(t1.Permute(fo))
		t0 = t0.ShiftLeft(sh2)
		t1 = t1.ShiftLeft(sh2)
		t0 = t0.Or(t0.Permute(fp))
		t1 = t1.Or(t1.Permute(fp))
		a0 := t0.GetLo().GetLo().GetElem(0)
		a1 := t0.GetHi().GetLo().GetElem(0)
		b0 := t1.GetLo().GetLo().GetElem(0)
		b1 := t1.GetHi().GetLo().GetElem(0)
		binary.LittleEndian.PutUint64(dst[off:], a0|a1<<(4*w))
		binary.LittleEndian.PutUint64(dst[off+8:], a1>>(64-4*w))
		binary.LittleEndian.PutUint64(dst[off+uint(w):], b0|b1<<(4*w))
		binary.LittleEndian.PutUint64(dst[off+uint(w)+8:], b1>>(64-4*w))
		off += 2 * uint(w)
	}
	for ; j < len(src); j++ {
		u := unsafecast.Slice[uint32](src[j][:])
		t := archsimd.LoadUint32x8Slice(u).ExtendToUint64().ShiftLeft(sh1)
		t = t.Or(t.Permute(fo))
		t = t.ShiftLeft(sh2)
		t = t.Or(t.Permute(fp))
		q0 := t.GetLo().GetLo().GetElem(0)
		q1 := t.GetHi().GetLo().GetElem(0)
		binary.LittleEndian.PutUint64(dst[off:], q0|q1<<(4*w))
		binary.LittleEndian.PutUint64(dst[off+8:], q1>>(64-4*w))
		off += uint(w)
	}
	archsimd.ClearAVXUpperBits()
	return int(off)
}

// encodeBytesBitpackSIMD packs 8 byte values per input word into bitWidth
// bits each, replacing the PDEP kernel of the assembly. Each 64 byte load
// covers 8 words; three fold levels narrow the byte lanes in place
// (a|b<<8 -> a|b<<w at 16, 32 and 64 bit granularity, all shifts with
// broadcast counts to avoid the legacy MOVQ of the scalar shift forms), and
// one VPERMB compacts the 8 groups of bitWidth bytes for a single store.
// The full 64 byte stores require room past the packed output, so the last
// words fall back to the scalar loop, which only overruns by the 8 bytes
// the callers guarantee (see encodeBytesBitpackDefault).
func encodeBytesBitpackSIMD(dst []byte, src []uint64, bitWidth uint) int {
	if bitWidth == 8 {
		return copy(dst, unsafecast.Slice[byte](src))
	}
	w := bitWidth
	bitMask := uint64(1<<w) - 1
	cp := archsimd.LoadUint8x64Slice(bitpackCompact[w][:])
	vmask := archsimd.BroadcastUint8x64(uint8(bitMask))
	m16 := archsimd.BroadcastUint16x32(0x00FF)
	s16r := archsimd.BroadcastUint16x32(8)
	s16l := archsimd.BroadcastUint16x32(uint16(w))
	m32 := archsimd.BroadcastUint32x16(0x0000FFFF)
	s32r := archsimd.BroadcastUint32x16(16)
	s32l := archsimd.BroadcastUint32x16(uint32(2 * w))
	m64 := archsimd.BroadcastUint64x8(0x00000000FFFFFFFF)
	s64r := archsimd.BroadcastUint64x8(32)
	s64l := archsimd.BroadcastUint64x8(uint64(4 * w))
	b := unsafecast.Slice[byte](src)
	n := 0
	j := 0
	for ; j+64 <= len(b) && n+64 <= len(dst); j += 64 {
		c := archsimd.LoadUint8x64Slice(b[j : j+64]).And(vmask)
		t16 := c.AsUint16x32()
		t16 = t16.And(m16).Or(t16.ShiftRight(s16r).ShiftLeft(s16l))
		t32 := t16.AsUint32x16()
		t32 = t32.And(m32).Or(t32.ShiftRight(s32r).ShiftLeft(s32l))
		t64 := t32.AsUint64x8()
		t64 = t64.And(m64).Or(t64.ShiftRight(s64r).ShiftLeft(s64l))
		t64.AsUint8x64().Permute(cp).StoreSlice(dst[n : n+64])
		n += int(8 * w)
	}
	archsimd.ClearAVXUpperBits()
	for _, word := range src[j/8:] {
		word = (word & bitMask) |
			(((word >> 8) & bitMask) << (1 * w)) |
			(((word >> 16) & bitMask) << (2 * w)) |
			(((word >> 24) & bitMask) << (3 * w)) |
			(((word >> 32) & bitMask) << (4 * w)) |
			(((word >> 40) & bitMask) << (5 * w)) |
			(((word >> 48) & bitMask) << (6 * w)) |
			(((word >> 56) & bitMask) << (7 * w))
		binary.LittleEndian.PutUint64(dst[n:], word)
		n += int(w)
	}
	return int(uint(len(src)) * w)
}

// decodeBytesBitpackSIMD expands bitWidth bit fields into bytes, replacing
// the PEXT kernel of the assembly. Two VPERMB gathers place the byte pair
// containing each field into a 16 bit lane, per lane variable shifts align
// the field, a mask isolates it, and a two source byte permute compacts the
// low bytes of the 64 lanes into the output vector. The index and shift
// tables depend only on bitWidth and are built once per call. Full 64 byte
// loads read past the consumed input, so the loop requires 64 readable
// bytes and leaves the tail to the scalar path.
func decodeBytesBitpackSIMD(dst, src []byte, count, bitWidth uint) {
	w := bitWidth
	if w == 8 {
		copy(dst, src[:count])
		return
	}
	bitMask := uint64(1<<w) - 1
	t := &bitpackUnpack[w]
	ia := archsimd.LoadUint8x64Slice(t.idxA[:])
	ib := archsimd.LoadUint8x64Slice(t.idxB[:])
	cm := archsimd.LoadUint8x64Slice(bitpackInterleave[:])
	sa := archsimd.LoadUint16x32Slice(t.shA[:])
	sb := archsimd.LoadUint16x32Slice(t.shB[:])
	vmask := archsimd.BroadcastUint16x32(uint16(bitMask))
	i := 0
	o := 0
	for ; count >= 64 && i+64 <= len(src); count -= 64 {
		c := archsimd.LoadUint8x64Slice(src[i : i+64])
		va := c.Permute(ia).AsUint16x32().ShiftRight(sa).And(vmask)
		vb := c.Permute(ib).AsUint16x32().ShiftRight(sb).And(vmask)
		va.AsUint8x64().ConcatPermute(vb.AsUint8x64(), cm).StoreSlice(dst[o : o+64])
		i += int(8 * w)
		o += 64
	}
	archsimd.ClearAVXUpperBits()
	if count >= 64 {
		// The loop consumes 8*bitWidth bytes per iteration but loads 64, so
		// the last iterations would read past the input. Copy the (at most
		// 64) remaining bytes into a zero padded buffer and recurse once:
		// the padded length keeps the recursion entirely in the vector
		// loop above. A second inline loop would share the table vectors
		// with the first and make the register allocator spill them on
		// every iteration of both (measured 3.5x on the whole kernel).
		var buf [128]byte
		copy(buf[:], src[i:])
		decodeBytesBitpackSIMD(dst[o:], buf[:], count, w)
		return
	}
	for ; count > 0; count -= 8 {
		var bits [8]byte
		copy(bits[:], src[i:min(i+int(w), len(src))])
		word := binary.LittleEndian.Uint64(bits[:])
		for e := range 8 {
			dst[o] = byte((word >> (uint(e) * w)) & bitMask)
			o++
		}
		i += int(w)
	}
}

// Permute and shift tables for the byte bit-packers, precomputed per bit
// width: building them per call dominated the runtime for small inputs
// (the first benchmark round measured up to +1550% on decode from the per
// call setup alone).
var (
	bitpackCompact    [8][64]uint8
	bitpackUnpack     [8]bitpackUnpackTables
	bitpackInterleave [64]uint8
)

type bitpackUnpackTables struct {
	idxA, idxB [64]uint8
	shA, shB   [32]uint16
}

func init() {
	for w := uint(1); w < 8; w++ {
		for m := range 8 * w {
			bitpackCompact[w][m] = uint8((m/w)*8 + m%w)
		}
		t := &bitpackUnpack[w]
		for m := range 64 {
			q, e := m/8, m%8
			bit := uint(e) * w
			b := uint(q)*w + bit/8
			if m < 32 {
				t.idxA[2*m] = uint8(b)
				t.idxA[2*m+1] = uint8(b + 1)
				t.shA[m] = uint16(bit % 8)
			} else {
				t.idxB[2*(m-32)] = uint8(b)
				t.idxB[2*(m-32)+1] = uint8(b + 1)
				t.shB[m-32] = uint16(bit % 8)
			}
		}
	}
	for m := range 64 {
		if m < 32 {
			bitpackInterleave[m] = uint8(2 * m)
		} else {
			bitpackInterleave[m] = uint8(64 + 2*(m-32))
		}
	}
}
