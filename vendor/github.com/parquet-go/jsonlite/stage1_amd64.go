//go:build goexperiment.simd && amd64

package jsonlite

import (
	"simd/archsimd"
	"unsafe"

	"github.com/parquet-go/bitpack/unsafecast"
)

// simdStage1 reports whether the vectorized structural indexer is available.
// The feature check is a cheap branch on a package variable, and will be
// erased by dead-code elimination under GOAMD64=v3 once
// https://go.dev/cl/813420 lands.
func simdStage1() bool { return archsimd.X86.AVX2() }

// structuralIndex scans s and appends emitted positions to index, returning
// the index, document-level flags, and any string-level validation error.
//
// There is deliberately no AVX-512 indexer: a 512-bit variant using one
// compare per character class measured no faster than the AVX2 kernel below
// on Ice Lake (2.17 vs 2.22 GB/s) — the nibble-shuffle classification makes
// up for the narrower vectors, and the AVX2 kernel runs on every CPU since
// Haswell. If stage 1 ever needs to go faster, port the nibble
// classification to 512-bit vectors rather than resurrecting the old
// kernel.
func structuralIndex(s string, index []uint32) ([]uint32, stage1Flags, error) {
	if archsimd.X86.AVX2() {
		return structuralIndexAVX2(s, index)
	}
	return structuralIndexPortable(s, index)
}

// Classification tables for the AVX2 indexer, from simdjson's character
// block classifier: a byte b is whitespace iff b == wsTable[b&0xF], and
// structural iff (b|0x20) == opTable[b&0xF] (the |0x20 folds '[' and ']'
// onto '{' and '}'). Filler values are chosen so no byte with that low
// nibble can match. The false positives (0x0C and 0x1A classify as
// structural) are control characters, which are invalid outside strings in
// any case and masked inside strings, so the accepted language is unchanged.
// The 16-entry tables are repeated per 128-bit lane for VPSHUFB.
var wsTable = [32]byte{
	0x20, 100, 100, 100, 17, 100, 113, 2, 100, 0x09, 0x0A, 112, 100, 0x0D, 100, 100,
	0x20, 100, 100, 100, 17, 100, 113, 2, 100, 0x09, 0x0A, 112, 100, 0x0D, 100, 100,
}

var opTable = [32]byte{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x3A, 0x7B, 0x2C, 0x7D, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x3A, 0x7B, 0x2C, 0x7D, 0, 0,
}

// structuralIndexAVX2 is the 256-bit structural indexer for CPUs without
// AVX-512. Each 64-byte block is processed as two halves whose movemasks are
// stitched into the 64-bit block masks. AVX2 has no unsigned byte compares,
// so the control and non-ASCII classes use signed compares (bytes >= 0x80
// are negative and sort below 0x20), and whitespace/structural chars are
// classified with nibble table shuffles instead of one compare per
// character, which also keeps the constants within AVX2's 16 vector
// registers.
func structuralIndexAVX2(s string, index []uint32) ([]uint32, stage1Flags, error) {
	var st stage1State
	st.prevSep = 1

	quote := archsimd.BroadcastUint8x32('"')
	backslash := archsimd.BroadcastUint8x32('\\')
	space := archsimd.BroadcastUint8x32(0x20)
	zeroInt := archsimd.BroadcastInt8x32(0)
	ctrlInt := archsimd.BroadcastInt8x32(0x20)
	lowNibble := archsimd.BroadcastUint8x32(0x0F)
	ws := archsimd.LoadUint8x32(&wsTable)
	op := archsimd.LoadUint8x32(&opTable)

	buf := unsafe.Slice(unsafe.StringData(s), len(s))
	blocks := unsafecast.Slice[[64]byte](buf)
	for bi := range blocks {
		b := &blocks[bi]
		lo := archsimd.LoadUint8x32((*[32]byte)(b[0:32]))
		hi := archsimd.LoadUint8x32((*[32]byte)(b[32:64]))

		var m blockMasks
		m.quote = uint64(lo.Equal(quote).ToBits()) |
			uint64(hi.Equal(quote).ToBits())<<32
		m.bs = uint64(lo.Equal(backslash).ToBits()) |
			uint64(hi.Equal(backslash).ToBits())<<32
		m.hi = uint64(lo.AsInt8x32().Less(zeroInt).ToBits()) |
			uint64(hi.AsInt8x32().Less(zeroInt).ToBits())<<32
		m.ctrl = (uint64(lo.AsInt8x32().Less(ctrlInt).ToBits()) |
			uint64(hi.AsInt8x32().Less(ctrlInt).ToBits())<<32) &^ m.hi
		m.ws = uint64(lo.Equal(ws.PermuteOrZeroGrouped(lo.And(lowNibble).AsInt8x32())).ToBits()) |
			uint64(hi.Equal(ws.PermuteOrZeroGrouped(hi.And(lowNibble).AsInt8x32())).ToBits())<<32
		m.structural = uint64(lo.Or(space).Equal(op.PermuteOrZeroGrouped(lo.And(lowNibble).AsInt8x32())).ToBits()) |
			uint64(hi.Or(space).Equal(op.PermuteOrZeroGrouped(hi.And(lowNibble).AsInt8x32())).ToBits())<<32

		index = st.crunch(m, bi*64, index)
	}
	i := len(blocks) * 64
	if i < len(s) {
		var b [64]byte
		for j := range b {
			b[j] = ' '
		}
		copy(b[:], s[i:])
		index = st.crunch(classifyBlockPortable(&b), i, index)
	}
	if st.err != nil {
		return index, st.flags(), st.err
	}
	if st.prevInString != 0 {
		return index, st.flags(), errUnterminatedString
	}
	return index, st.flags(), nil
}
