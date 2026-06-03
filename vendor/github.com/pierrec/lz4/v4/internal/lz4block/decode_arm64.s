// +build gc
// +build !noasm

// This implementation assumes that strict alignment checking is turned off.
// The Go compiler makes the same assumption.

#include "go_asm.h"
#include "textflag.h"

// Register allocation.
#define dst		R0
#define dstorig		R1
#define src		R2
#define dstend		R3
#define dstend16	R4	// dstend - 16
#define srcend		R5
#define srcend16	R6	// srcend - 16
#define match		R7	// Match address.
#define dict		R8
#define dictlen		R9
#define dictend		R10
#define token		R11
#define len		R12	// Literal and match lengths.
#define lenRem		R13
#define offset		R14	// Match offset.
#define tmp1		R15
#define tmp2		R16
#define tmp3		R17
#define tmp4		R19
#define dstend32	R20	// dstend - 32 (shortcut guard)

// func decodeBlock(dst, src, dict []byte) int
//
// Frame: 48 bytes for calling runtime·memmove in the long-match fast path
// (arg0..arg2 at 0/8/16(SP), spill of dst-after-match and src at 24/32(SP)).
// NOSPLIT is preserved -- memmove's own stack use is well under the nosplit
// margin.
TEXT ·decodeBlock(SB), NOSPLIT, $48-80
	LDP  dst_base+0(FP), (dst, dstend)
	ADD  dst, dstend
	MOVD dst, dstorig

	LDP src_base+24(FP), (src, srcend)
	CBZ srcend, shortSrc
	ADD src, srcend

	// dstend16 = max(dstend-16, 0) and similarly for dstend32, srcend16.
	SUBS $16, dstend, dstend16
	CSEL LO, ZR, dstend16, dstend16
	SUBS $32, dstend, dstend32
	CSEL LO, ZR, dstend32, dstend32
	SUBS $16, srcend, srcend16
	CSEL LO, ZR, srcend16, srcend16

	LDP dict_base+48(FP), (dict, dictlen)
	ADD dict, dictlen, dictend

loop:
	// Read token. Extract literal length.
	MOVBU.P 1(src), token
	LSR     $4, token, len
	CMP     $15, len
	BEQ     readLitlenLoop        // len == 15: extended read, slow path.

	// Shortcut: literal length is 0..14. If we also have at least 32 bytes
	// of dst and 16 bytes of src remaining, copy 16 literal bytes in one
	// shot, then try to finish the token's match with an 18-byte copy.
	// Falls back to the slow path on any guard failure. Mirrors the
	// "copy shortcut" in decode_amd64.s.
	CMP dstend32, dst
	BHS readLitlenDone            // <32 bytes left in dst: slow path.
	CMP srcend16, src
	BHS readLitlenDone            // <16 bytes left in src: slow path.

	// 16-byte literal copy (bytes past len get overwritten next iter).
	LDP (src), (tmp1, tmp2)
	STP (tmp1, tmp2), (dst)
	ADD len, src
	ADD len, dst

	// Derive initial matchlen from token's low nibble.
	AND $15, token, len

	// Read 2-byte offset (src has >=2 bytes left by the guard above).
	MOVHU (src), offset
	ADD   $2, src
	CBZ   offset, corrupt

	// Fast-match preconditions: matchlen != 15, offset >= 8, match is
	// within the current block (>= dstorig -- not a dict reference).
	CMP $15, len
	BEQ readMatchlen              // extended matchlen: slow path.
	CMP $8, offset
	BLO readMatchlen              // small offset: use existing <8 path.
	SUB offset, dst, match
	CMP dstorig, match
	BLO readMatchlen              // dict reference: use existing dict path.

	// 18-byte match copy. dst-space is guaranteed: dst < dstend-32 and
	// matchlen+minMatch <= 18. Bytes past matchlen+minMatch get
	// overwritten next iter.
	//
	// For offset >= 18 there is no aliasing between [match, match+17]
	// and [dst, dst+17], so LDP+STP can load all 16 bytes in parallel
	// before any store retires -- 4 memory ops total instead of 6.
	// Measurements on kibble-sourced columnar data (int64c/float64c/
	// varstring.dictc/hexc) show ~95% of all matches have offset >= 18,
	// so this is the common case.
	//
	// For offset 8..17 an LDP reads past the first store's destination,
	// so we fall back to sequenced 8+8+2 so that each load observes
	// the prior store's effect (required for offset == 8 RLE cycling).
	CMP   $18, offset
	BLO   shortcutMatchSerial

	LDP   (match), (tmp1, tmp2)
	MOVHU 16(match), tmp3
	STP   (tmp1, tmp2), (dst)
	MOVH  tmp3, 16(dst)
	ADD   $const_minMatch, len
	ADD   len, dst
	B     copyMatchDone

shortcutMatchSerial:
	MOVD  (match), tmp1
	MOVD  tmp1, (dst)
	MOVD  8(match), tmp2
	MOVD  tmp2, 8(dst)
	MOVHU 16(match), tmp3
	MOVH  tmp3, 16(dst)
	ADD   $const_minMatch, len
	ADD   len, dst
	B     copyMatchDone

readLitlenLoop:
	CMP     src, srcend
	BEQ     shortSrc
	MOVBU.P 1(src), tmp1
	ADDS    tmp1, len
	BVS     shortDst
	CMP     $255, tmp1
	BEQ     readLitlenLoop

readLitlenDone:
	CBZ len, copyLiteralDone

	// Bounds check dst+len and src+len.
	ADDS dst, len, tmp1
	BCS  shortSrc
	ADDS src, len, tmp2
	BCS  shortSrc
	CMP  dstend, tmp1
	BHI  shortDst
	CMP  srcend, tmp2
	BHI  shortSrc

	// Copy literal.
	SUBS $16, len
	BLO  copyLiteralShort

copyLiteralLoop:
	LDP.P 16(src), (tmp1, tmp2)
	STP.P (tmp1, tmp2), 16(dst)
	SUBS  $16, len
	BPL   copyLiteralLoop

	// Copy (final part of) literal of length 0-15.
	// If we have >=16 bytes left in src and dst, just copy 16 bytes.
copyLiteralShort:
	CMP  dstend16, dst
	CCMP LO, src, srcend16, $0b0010 // 0010 = preserve carry (LO).
	BHS  copyLiteralShortEnd

	AND $15, len

	LDP (src), (tmp1, tmp2)
	ADD len, src
	STP (tmp1, tmp2), (dst)
	ADD len, dst

	B copyLiteralDone

	// Safe but slow copy near the end of src, dst.
copyLiteralShortEnd:
	TBZ     $3, len, 3(PC)
	MOVD.P  8(src), tmp1
	MOVD.P  tmp1, 8(dst)
	TBZ     $2, len, 3(PC)
	MOVW.P  4(src), tmp2
	MOVW.P  tmp2, 4(dst)
	TBZ     $1, len, 3(PC)
	MOVH.P  2(src), tmp3
	MOVH.P  tmp3, 2(dst)
	TBZ     $0, len, 3(PC)
	MOVBU.P 1(src), tmp4
	MOVB.P  tmp4, 1(dst)

copyLiteralDone:
	// Initial part of match length.
	AND $15, token, len

	CMP src, srcend
	BEQ end

	// Read offset.
	ADDS  $2, src
	BCS   shortSrc
	CMP   srcend, src
	BHI   shortSrc
	MOVHU -2(src), offset
	CBZ   offset, corrupt

readMatchlen:
	// Read rest of match length.
	CMP $15, len
	BNE readMatchlenDone

readMatchlenLoop:
	CMP     src, srcend
	BEQ     shortSrc
	MOVBU.P 1(src), tmp1
	ADDS    tmp1, len
	BVS     shortDst
	CMP     $255, tmp1
	BEQ     readMatchlenLoop

readMatchlenDone:
	ADD $const_minMatch, len

	// Bounds check dst+len.
	ADDS dst, len, tmp2
	BCS  shortDst
	CMP  dstend, tmp2
	BHI  shortDst

	SUB offset, dst, match
	CMP dstorig, match
	BHS copyMatchTry8

	// match < dstorig means the match starts in the dictionary,
	// at len(dict) - offset + (dst - dstorig).
	SUB  dstorig, dst, tmp1
	SUB  offset, dictlen, tmp2
	ADDS tmp2, tmp1
	BMI  shortDict
	ADD  dict, tmp1, match

copyDict:
	MOVBU.P 1(match), tmp3
	MOVB.P  tmp3, 1(dst)
	SUBS    $1, len
	CCMP    NE, dictend, match, $0b0100 // 0100 sets the Z (EQ) flag.
	BNE     copyDict

	CBZ len, copyMatchDone

	// If the match extends beyond the dictionary, the rest is at dstorig.
	// Recompute the offset for the next check.
	MOVD dstorig, match
	SUB  dstorig, dst, offset

copyMatchTry8:
	// Non-overlapping bulk copy: len >= 64 and offset >= len means the
	// whole match can be served by runtime.memmove, whose arm64
	// implementation uses 128-bit NEON with prefetch and outruns any
	// 16B/iter LDP+STP loop we can realistically write inline. Columnar
	// / record-oriented workloads hit this case heavily (large offsets,
	// match length ~record size). Below 64 bytes the call-and-spill
	// overhead dominates, so the inline LDP/STP loop below stays.
	CMP  $256, len
	BLO  copyMatchTry8_inline
	CMP  len, offset
	BLO  copyMatchTry8_inline      // offset < len -> match cycles, can't memmove.
	B    copyMatchViaMemmove

copyMatchTry8_inline:
	// Copy quadwords (16 bytes/iter via LDP/STP) if len and offset are both
	// large enough. offset >= 32 guarantees there is no store-to-load
	// forwarding dependency between iterations: iter N+1's LDP reads bytes
	// at (match+16) which, since match = dst - offset, sits at
	// dst - (offset-16). For offset >= 32 that is pre-dst, untouched by
	// iter N's STP. Kibble columnar data (int64c/float64c/varstring.dictc/
	// hexc) shows ~81% of matches with len >= 19 have offset >= 32, and
	// those long matches produce ~75% of total match-copy bytes, so this
	// is the dominant path for columnar workloads.
	// CCMP immediate is 5-bit unsigned (0..31); we can't encode $32 directly,
	// so compare offset against $31 with BLS (lower or same) to match
	// "offset < 32" exactly. The first-CMP "len < 16" case falls into BLS
	// via NZCV=$0 (C=0 => LS).
	CMP  $16, len
	CCMP HS, offset, $31, $0
	BLS  copyMatchTry8Narrow

	AND    $15, len, lenRem
	SUB    $16, len
copyMatchLoop16:
	LDP.P 16(match), (tmp1, tmp2)
	STP.P (tmp1, tmp2), 16(dst)
	SUBS   $16, len
	BPL    copyMatchLoop16

	// LDP lacks a (base)(index) addressing mode, so compute match+len
	// into a scratch register first.
	ADD  match, len, tmp3           // tmp3 = match + lenRem - 16
	LDP  (tmp3), (tmp1, tmp2)
	ADD  lenRem, dst
	MOVD $0, len
	STP  (tmp1, tmp2), -16(dst)
	B    copyMatchDone

copyMatchTry8Narrow:
	// 8-byte path for len >= 8 and offset in [8, 32). Preserves existing
	// behavior for small offsets where the 16B loop would either alias
	// (offset < 16) or incur per-iter STLF stalls (offset 16..31).
	CMP  $8, len
	CCMP HS, offset, $8, $0
	BLO  copyMatchTry4

	AND    $7, len, lenRem
	SUB    $8, len
copyMatchLoop8:
	MOVD.P 8(match), tmp1
	MOVD.P tmp1, 8(dst)
	SUBS   $8, len
	BPL    copyMatchLoop8

	MOVD (match)(len), tmp2 // match+len == match+lenRem-8.
	ADD  lenRem, dst
	MOVD $0, len
	MOVD tmp2, -8(dst)
	B    copyMatchDone

copyMatchTry4:
	// Copy words if both len and offset are at least four.
	CMP  $4, len
	CCMP HS, offset, $4, $0
	BLO  copyMatchLoop1

	MOVWU.P 4(match), tmp2
	MOVWU.P tmp2, 4(dst)
	SUBS    $4, len
	BEQ     copyMatchDone

copyMatchLoop1:
	// For offset == 1 or 2 and len >= 8, splat the 1- or 2-byte pattern
	// into tmp3 and store 8 bytes at a time. The remaining 1..7 bytes,
	// plus offset == 3 and the len < 8 cases, fall through to the
	// byte-by-byte loop below. match is not advanced during the splat;
	// the byte loop's post-index read correctly observes the pattern
	// because match[k] for k >= offset sees the bytes we just splatted.
	CMP $8, len
	BLO copyMatchByteLoop         // len < 8: byte loop is shorter.
	CMP $2, offset
	BHI copyMatchByteLoop         // offset == 3: period doesn't tile 8B.
	BEQ copyMatchSplat2           // offset == 2

	// offset == 1: splat a single byte to all 8 bytes of tmp3.
	MOVBU (match), tmp3
	ORR   tmp3<<8, tmp3, tmp3
	ORR   tmp3<<16, tmp3, tmp3
	B     copyMatchSplatTile32

copyMatchSplat2:
	// offset == 2: splat a halfword.
	MOVHU (match), tmp3
	ORR   tmp3<<16, tmp3, tmp3

copyMatchSplatTile32:
	ORR tmp3<<32, tmp3, tmp3

	// 16-byte store-pair loop for the bulk. G2 onwards (Neoverse N1 and
	// every later core, and Apple M-series) can retire an STP of two
	// X-registers as a single 16-byte store; doubling the store width
	// halves the iteration count and the per-iteration loop overhead.
copyMatchSplatLoop:
	CMP    $16, len
	BLO    copyMatchSplatTail
	STP.P  (tmp3, tmp3), 16(dst)
	SUB    $16, len
	B      copyMatchSplatLoop

copyMatchSplatTail:
	// 0..15 bytes remain. If >= 8, emit one more 8-byte store.
	CBZ    len, copyMatchDone
	CMP    $8, len
	BLO    copyMatchByteLoop
	MOVD.P tmp3, 8(dst)
	SUBS   $8, len
	BEQ    copyMatchDone
	// fall through with 1..7 bytes remaining.

copyMatchByteLoop:
	// Byte-at-a-time copy for small offsets <= 3.
	MOVBU.P 1(match), tmp2
	MOVB.P  tmp2, 1(dst)
	SUBS    $1, len
	BNE     copyMatchByteLoop

copyMatchDone:
	CMP src, srcend
	BNE loop

	B end

copyMatchViaMemmove:
	// runtime·memmove(dst, match, len). Caller must guarantee offset >= len
	// (non-overlapping) and a stack frame of at least 48 bytes.
	//
	// Go's arm64 ABI0 places a callee's args at caller_SP + 8 (the 0
	// offset is reserved for the callee's LR save slot), so arg0..arg2 go
	// at 8/16/24(RSP) from our POV. The callee may clobber R0..R18 and
	// most NEON regs; only R19..R28 and V8..V15 are preserved. Every
	// working register we use is caller-save from memmove's perspective,
	// so we spill the advanced dst and src to the frame and rebuild
	// dstend/dstend16/dstend32/srcend/srcend16/dict/dictlen/dictend from
	// the FP slots after the call.
	MOVD dst, 8(RSP)               // memmove arg0: to
	MOVD match, 16(RSP)            // memmove arg1: from
	MOVD len, 24(RSP)              // memmove arg2: n
	ADD  len, dst, dst             // post-match dst position
	MOVD dst, 32(RSP)              // spill advanced dst
	MOVD src, 40(RSP)              // spill src
	BL   runtime·memmove(SB)
	MOVD 32(RSP), dst
	MOVD 40(RSP), src

	// Rebuild derived pointers/ends. dstorig := dst_base; dstend := dst_base + dst_len; etc.
	MOVD dst_base+0(FP), dstorig
	MOVD dst_len+8(FP), dstend
	ADD  dstorig, dstend, dstend
	SUBS $16, dstend, dstend16
	CSEL LO, ZR, dstend16, dstend16
	SUBS $32, dstend, dstend32
	CSEL LO, ZR, dstend32, dstend32

	MOVD src_base+24(FP), tmp1
	MOVD src_len+32(FP), srcend
	ADD  tmp1, srcend, srcend
	SUBS $16, srcend, srcend16
	CSEL LO, ZR, srcend16, srcend16

	MOVD dict_base+48(FP), dict
	MOVD dict_len+56(FP), dictlen
	ADD  dict, dictlen, dictend

	MOVD $0, len
	B    copyMatchDone

end:
	CBNZ len, corrupt
	SUB  dstorig, dst, tmp1
	MOVD tmp1, ret+72(FP)
	RET

	// The error cases have distinct labels so we can put different
	// return codes here when debugging, or if the error returns need to
	// be changed.
shortDict:
shortDst:
shortSrc:
corrupt:
	MOVD $-1, tmp1
	MOVD tmp1, ret+72(FP)
	RET
