//go:build goexperiment.simd

package bloom

import (
	"simd/archsimd"

	"github.com/parquet-go/bitpack/unsafecast"
)

// This file provides implementations of the block and filter operations based
// on the simd/archsimd package, replacing the hand-written assembly of
// block_amd64.s and filter_amd64.s when GOEXPERIMENT=simd is set.

var (
	blockSalt  = [8]uint32{salt0, salt1, salt2, salt3, salt4, salt5, salt6, salt7}
	blockOnes  = [8]uint32{1, 1, 1, 1, 1, 1, 1, 1}
	blockShift = [8]uint32{27, 27, 27, 27, 27, 27, 27, 27}
)

// blockMask computes the 8 bit masks of the parquet split-block bloom filter:
// mask[i] = 1 << ((x * salt[i]) >> 27).
//
// The 27 shift uses the per-lane ShiftRight with a constant vector rather
// than ShiftAllRight(27): as of Go 1.26 the compiler materializes the scalar
// shift count of ShiftAllRight with a legacy SSE (non-VEX) MOVQ, and mixing
// legacy SSE with 256-bit VEX code pays an AVX/SSE state transition penalty
// on every call that makes the function ~60x slower.
func blockMask(x uint32) archsimd.Uint32x8 {
	salt := archsimd.LoadUint32x8Slice(blockSalt[:])
	return archsimd.LoadUint32x8Slice(blockOnes[:]).
		ShiftLeft(archsimd.BroadcastUint32x8(x).Mul(salt).
			ShiftRight(archsimd.LoadUint32x8Slice(blockShift[:])))
}

func (b *Block) Insert(x uint32) {
	if archsimd.X86.AVX2() {
		w := unsafecast.Slice[uint32](b[:])
		archsimd.LoadUint32x8Slice(w).Or(blockMask(x)).StoreSlice(w)
		archsimd.ClearAVXUpperBits()
		return
	}
	b[0] |= 1 << ((x * salt0) >> 27)
	b[1] |= 1 << ((x * salt1) >> 27)
	b[2] |= 1 << ((x * salt2) >> 27)
	b[3] |= 1 << ((x * salt3) >> 27)
	b[4] |= 1 << ((x * salt4) >> 27)
	b[5] |= 1 << ((x * salt5) >> 27)
	b[6] |= 1 << ((x * salt6) >> 27)
	b[7] |= 1 << ((x * salt7) >> 27)
}

func (b *Block) Check(x uint32) bool {
	if archsimd.X86.AVX2() {
		m := blockMask(x)
		w := unsafecast.Slice[uint32](b[:])
		ok := archsimd.LoadUint32x8Slice(w).And(m).Equal(m).ToBits() == 0xFF
		archsimd.ClearAVXUpperBits()
		return ok
	}
	return ((b[0] & (1 << ((x * salt0) >> 27))) != 0) &&
		((b[1] & (1 << ((x * salt1) >> 27))) != 0) &&
		((b[2] & (1 << ((x * salt2) >> 27))) != 0) &&
		((b[3] & (1 << ((x * salt3) >> 27))) != 0) &&
		((b[4] & (1 << ((x * salt4) >> 27))) != 0) &&
		((b[5] & (1 << ((x * salt5) >> 27))) != 0) &&
		((b[6] & (1 << ((x * salt6) >> 27))) != 0) &&
		((b[7] & (1 << ((x * salt7) >> 27))) != 0)
}

func filterInsertBulk(f []Block, x []uint64) {
	if archsimd.X86.AVX2() {
		// Same structure as filterInsert, but with the constant vectors
		// hoisted out of the loop; the hash stays scalar (2 shifts and a
		// multiply in general purpose registers pipeline for free).
		salt := archsimd.LoadUint32x8Slice(blockSalt[:])
		ones := archsimd.LoadUint32x8Slice(blockOnes[:])
		shift := archsimd.LoadUint32x8Slice(blockShift[:])
		scale := int32(len(f))
		for _, h := range x {
			m := ones.ShiftLeft(archsimd.BroadcastUint32x8(uint32(h)).Mul(salt).ShiftRight(shift))
			w := unsafecast.Slice[uint32](f[fasthash1x64(h, scale)][:])
			archsimd.LoadUint32x8Slice(w).Or(m).StoreSlice(w)
		}
		archsimd.ClearAVXUpperBits()
		return
	}
	for i := range x {
		filterInsert(f, x[i])
	}
}

func filterInsert(f []Block, x uint64) {
	f[fasthash1x64(x, int32(len(f)))].Insert(uint32(x))
}

func filterCheck(f []Block, x uint64) bool {
	return f[fasthash1x64(x, int32(len(f)))].Check(uint32(x))
}
