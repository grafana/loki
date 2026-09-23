// +build !appengine
// +build gc
// +build !noasm

#include "go_asm.h"
#include "textflag.h"

// AX scratch
// BX scratch, match pointer
// CX literal and match lengths
// DX token, match offset
// R10 scratch (match copy)
// X0, X1 scratch (match copy)
//
// DI &dst
// SI &src
// R8 &dst + len(dst)
// R9 &src + len(src)
// R11 &dst
// R12 short output end
// R13 short input end
// R14 &dict
// R15 len(dict)

// func decodeBlock(dst, src, dict []byte) int
TEXT ·decodeBlock(SB), NOSPLIT, $48-80
	MOVQ dst_base+0(FP), DI
	MOVQ DI, R11
	MOVQ dst_len+8(FP), R8
	ADDQ DI, R8

	MOVQ src_base+24(FP), SI
	MOVQ src_len+32(FP), R9
	CMPQ R9, $0
	JE   err_corrupt
	ADDQ SI, R9

	MOVQ dict_base+48(FP), R14
	MOVQ dict_len+56(FP), R15

	// shortcut ends
	// short output end
	MOVQ R8, R12
	SUBQ $32, R12
	// short input end
	MOVQ R9, R13
	SUBQ $16, R13

	XORL CX, CX

loop:
	// token := uint32(src[si])
	MOVBLZX (SI), DX
	INCQ SI

	// lit_len = token >> 4
	// if lit_len > 0
	// CX = lit_len
	MOVL DX, CX
	SHRL $4, CX

	// if lit_len != 0xF
	CMPL CX, $0xF
	JEQ  lit_len_loop
	CMPQ DI, R12
	JAE  copy_literal
	CMPQ SI, R13
	JAE  copy_literal

	// copy shortcut

	// A two-stage shortcut for the most common case:
	// 1) If the literal length is 0..14, and there is enough space,
	// enter the shortcut and copy 16 bytes on behalf of the literals
	// (in the fast mode, only 8 bytes can be safely copied this way).
	// 2) Further if the match length is 4..18, copy 18 bytes in a similar
	// manner; but we ensure that there's enough space in the output for
	// those 18 bytes earlier, upon entering the shortcut (in other words,
	// there is a combined check for both stages).

	// copy literal
	MOVOU (SI), X0
	MOVOU X0, (DI)
	ADDQ CX, DI
	ADDQ CX, SI

	MOVL DX, CX
	ANDL $0xF, CX

	// The second stage: prepare for match copying, decode full info.
	// If it doesn't work out, the info won't be wasted.
	// offset := uint16(data[:2])
	MOVWLZX (SI), DX
	TESTL DX, DX
	JE err_corrupt
	ADDQ $2, SI
	JC err_short_buf

	MOVQ DI, AX
	SUBQ DX, AX
	JC err_corrupt
	CMPQ AX, DI
	JA err_short_buf

	// if we can't do the second stage then jump straight to read the
	// match length, we already have the offset.
	CMPL CX, $0xF
	JEQ match_len_loop_pre
	CMPL DX, $8
	JLT match_len_loop_pre
	CMPQ AX, R11
	JB match_len_loop_pre

	// memcpy(op + 0, match + 0, 8);
	MOVQ (AX), BX
	MOVQ BX, (DI)
	// memcpy(op + 8, match + 8, 8);
	MOVQ 8(AX), BX
	MOVQ BX, 8(DI)
	// memcpy(op +16, match +16, 2);
	MOVW 16(AX), BX
	MOVW BX, 16(DI)

	LEAQ const_minMatch(DI)(CX*1), DI

	// shortcut complete, load next token
	JMP loopcheck

	// Read the rest of the literal length:
	// do { BX = src[si++]; lit_len += BX } while (BX == 0xFF).
lit_len_loop:
	CMPQ SI, R9
	JAE err_short_buf

	MOVBLZX (SI), BX
	INCQ SI
	ADDQ BX, CX

	CMPB BX, $0xFF
	JE lit_len_loop

copy_literal:
	// bounds check src and dst
	MOVQ SI, AX
	ADDQ CX, AX
	JC err_short_buf
	CMPQ AX, R9
	JA err_short_buf

	MOVQ DI, BX
	ADDQ CX, BX
	JC err_short_buf
	CMPQ BX, R8
	JA err_short_buf

	// Copy literals of <=48 bytes through the XMM registers.
	CMPQ CX, $48
	JGT memmove_lit

	// if len(dst[di:]) < 48
	MOVQ R8, AX
	SUBQ DI, AX
	CMPQ AX, $48
	JLT memmove_lit

	// if len(src[si:]) < 48
	MOVQ R9, BX
	SUBQ SI, BX
	CMPQ BX, $48
	JLT memmove_lit

	MOVOU (SI), X0
	MOVOU 16(SI), X1
	MOVOU 32(SI), X2
	MOVOU X0, (DI)
	MOVOU X1, 16(DI)
	MOVOU X2, 32(DI)

	ADDQ CX, SI
	ADDQ CX, DI

	JMP finish_lit_copy

memmove_lit:
	// memmove(to, from, len)
	MOVQ DI, 0(SP)
	MOVQ SI, 8(SP)
	MOVQ CX, 16(SP)

	// Spill registers. Increment SI, DI now so we don't need to save CX.
	ADDQ CX, DI
	ADDQ CX, SI
	MOVQ DI, 24(SP)
	MOVQ SI, 32(SP)
	MOVL DX, 40(SP)

	CALL runtime·memmove(SB)

	// restore registers
	MOVQ 24(SP), DI
	MOVQ 32(SP), SI
	MOVL 40(SP), DX

	// recalc initial values
	MOVQ dst_base+0(FP), R8
	MOVQ R8, R11
	ADDQ dst_len+8(FP), R8
	MOVQ src_base+24(FP), R9
	ADDQ src_len+32(FP), R9
	MOVQ dict_base+48(FP), R14
	MOVQ dict_len+56(FP), R15
	MOVQ R8, R12
	SUBQ $32, R12
	MOVQ R9, R13
	SUBQ $16, R13

finish_lit_copy:
	// CX := mLen
	// free up DX to use for offset
	MOVL DX, CX
	ANDL $0xF, CX

	CMPQ SI, R9
	JAE end

	// offset
	// si += 2
	// DX := int(src[si-2]) | int(src[si-1])<<8
	ADDQ $2, SI
	JC err_short_buf
	CMPQ SI, R9
	JA err_short_buf
	MOVWQZX -2(SI), DX

	// 0 offset is invalid
	TESTL DX, DX
	JEQ   err_corrupt

match_len_loop_pre:
	// if mlen != 0xF
	CMPB CX, $0xF
	JNE copy_match

	// do { BX = src[si++]; mlen += BX } while (BX == 0xFF).
match_len_loop:
	CMPQ SI, R9
	JAE err_short_buf

	MOVBLZX (SI), BX
	INCQ SI
	ADDQ BX, CX

	CMPB BX, $0xFF
	JE match_len_loop

copy_match:
	ADDQ $const_minMatch, CX

	// check we have match_len bytes left in dst
	// di+match_len < len(dst)
	MOVQ DI, AX
	ADDQ CX, AX
	JC err_short_buf
	CMPQ AX, R8
	JA err_short_buf

	// DX = offset
	// CX = match_len
	// BX = &dst + (di - offset)
	MOVQ DI, BX
	SUBQ DX, BX

	// check BX is within dst
	// if BX < &dst
	JC copy_match_from_dict
	CMPQ BX, R11
	JB copy_match_from_dict

copy_match_dispatch:
	// DX offset, CX match_len > 0, BX match, DI dst. Also entered from
	// copy_match_from_dict with BX = &dst, DX = di.
	CMPQ DX, CX
	JB   copy_match_overlap

	// Non-overlapping match (offset >= match_len).
	CMPQ CX, $16
	JA   copy_match_nonoverlap_long

	// match_len <= 16: one 16-byte copy if there is room for the overrun.
	// if len(dst[di:]) < 16
	MOVQ R8, AX
	SUBQ DI, AX
	CMPQ AX, $16
	JB   copy_match_loop

	MOVOU (BX), X0
	MOVOU X0, (DI)

	ADDQ CX, DI
	XORL CX, CX
	JMP  loopcheck

copy_match_loop:
	// Byte copy: short overlapping matches and tails without room to overrun.
	// dst[di] = dst[i]
	// di++
	// i++
	MOVB (BX), AX
	MOVB AX, (DI)
	INCQ DI
	INCQ BX
	DECQ CX
	JNZ copy_match_loop

	JMP loopcheck

copy_match_nonoverlap_long:
	// 17..255 bytes: inline 16-byte loop, last chunk ends exactly at
	// di+match_len. 256 and up still call memmove.
	CMPQ CX, $256
	JAE  memmove_match

	MOVOU -16(BX)(CX*1), X1 // tail; the match does not overlap, so load it up front
	LEAQ  -16(DI)(CX*1), R10
copy_match_nonoverlap_loop:
	MOVOU (BX), X0
	MOVOU X0, (DI)
	ADDQ  $16, BX
	ADDQ  $16, DI
	CMPQ  DI, R10
	JB    copy_match_nonoverlap_loop

	MOVOU X1, (R10)
	LEAQ  16(R10), DI
	XORL  CX, CX
	JMP   loopcheck

copy_match_overlap:
	// Overlapping: the output is periodic with period offset.
	CMPQ DX, $32
	JAE  copy_match_overlap32
	CMPQ DX, $16
	JA   copy_match_overlap17 // 17..31
	JE   copy_match_splat16

	// offset < 16: byte loop if short, else splat (1, 2, 4, 8) or tile.
	CMPQ CX, $16
	JB   copy_match_loop
	CMPQ DX, $8
	JA   copy_match_tile // 9..15
	JE   copy_match_splat8
	CMPQ DX, $4
	JA   copy_match_tile // 5, 6, 7
	JE   copy_match_splat4
	CMPQ DX, $2
	JA   copy_match_tile // 3
	JE   copy_match_splat2

	// offset == 1: replicate the byte across X0.
	MOVBQZX    (BX), AX
	MOVQ       $0x0101010101010101, R10
	IMULQ      R10, AX
	MOVQ       AX, X0
	PUNPCKLQDQ X0, X0
	MOVQ       $16, R10
	JMP        copy_match_tile_loop

copy_match_splat2:
	MOVWQZX    (BX), AX
	MOVQ       $0x0001000100010001, R10
	IMULQ      R10, AX
	MOVQ       AX, X0
	PUNPCKLQDQ X0, X0
	MOVQ       $16, R10
	JMP        copy_match_tile_loop

copy_match_splat4:
	MOVL   (BX), X0
	PSHUFD $0, X0, X0
	MOVQ   $16, R10
	JMP    copy_match_tile_loop

copy_match_splat8:
	MOVQ       (BX), X0
	PUNPCKLQDQ X0, X0
	MOVQ       $16, R10
	JMP        copy_match_tile_loop

copy_match_splat16:
	MOVOU (BX), X0
	MOVQ  $16, R10
	JMP   copy_match_tile_loop

copy_match_tile:
	// Prefill step = (16/offset)*offset bytes so [match, di) holds a full
	// tile and di is phase-aligned; then store the tile every step bytes.
	LEAQ    tileStep<>(SB), R10
	MOVBQZX (R10)(DX*1), R10
	SUBQ    R10, CX
	LEAQ    (DI)(R10*1), AX // prefill end
copy_match_tile_prefill:
	MOVB (BX), R10
	MOVB R10, (DI)
	INCQ BX
	INCQ DI
	CMPQ DI, AX
	JB   copy_match_tile_prefill

	LEAQ    tileStep<>(SB), R10
	MOVBQZX (R10)(DX*1), R10
	MOVQ    DI, AX
	SUBQ    R10, AX
	SUBQ    DX, AX // AX = match: the tile source
	MOVOU   (AX), X0

copy_match_tile_loop:
	// X0 tile, R10 step (16 for splats), CX bytes left.
	CMPQ  CX, $16
	JB    copy_match_tile_tail
	MOVOU X0, (DI)
	ADDQ  R10, DI
	SUBQ  R10, CX
	JMP   copy_match_tile_loop

copy_match_tile_tail:
	// 0..15 left: one more tile if it fits in dst, else bytes.
	TESTQ CX, CX
	JZ    loopcheck
	MOVQ  R8, AX
	SUBQ  DI, AX
	CMPQ  AX, $16
	JB    copy_match_tail_bytes
	MOVOU X0, (DI)
	ADDQ  CX, DI
	XORL  CX, CX
	JMP   loopcheck

copy_match_tail_bytes:
	MOVQ DI, BX
	SUBQ DX, BX
	JMP  copy_match_loop

copy_match_overlap17:
	// 17..31: prefill one period with two 16-byte copies (both inside
	// [di, di+offset)), then store the 32-byte tile at match every offset bytes.
	MOVOU (BX), X0
	MOVOU X0, (DI)
	MOVOU -16(BX)(DX*1), X1
	MOVOU X1, -16(DI)(DX*1)
	ADDQ  DX, DI
	SUBQ  DX, CX
	MOVOU 16(BX), X1

copy_match_overlap17_loop:
	CMPQ  CX, $32
	JB    copy_match_overlap17_tail
	MOVOU X0, (DI)
	MOVOU X1, 16(DI)
	ADDQ  DX, DI
	SUBQ  DX, CX
	JMP   copy_match_overlap17_loop

copy_match_overlap17_tail:
	// 0..31 left: one more tile if it fits in dst, else bytes.
	TESTQ CX, CX
	JZ    loopcheck
	MOVQ  R8, AX
	SUBQ  DI, AX
	CMPQ  AX, $32
	JB    copy_match_tail_bytes
	MOVOU X0, (DI)
	MOVOU X1, 16(DI)
	ADDQ  CX, DI
	XORL  CX, CX
	JMP   loopcheck

copy_match_overlap32:
	// offset >= 32: loads never touch the previous iteration's store. The
	// last chunk ends exactly at di+match_len and is loaded after the loop.
	LEAQ -16(DI)(CX*1), R10
	LEAQ -16(BX)(CX*1), AX
copy_match_overlap32_loop:
	MOVOU (BX), X0
	MOVOU X0, (DI)
	ADDQ  $16, BX
	ADDQ  $16, DI
	CMPQ  DI, R10
	JB    copy_match_overlap32_loop

	MOVOU (AX), X1
	MOVOU X1, (R10)
	LEAQ  16(R10), DI
	XORL  CX, CX
	JMP   loopcheck

copy_match_from_dict:
	// CX = match_len
	// BX = &dst + (di - offset)

	// AX = offset - di = dict_bytes_available => count of bytes potentially covered by the dictionary
	MOVQ R11, AX
	SUBQ BX, AX

	// BX = len(dict) - dict_bytes_available
	MOVQ R15, BX
	SUBQ AX, BX
	JS err_short_dict

	ADDQ R14, BX

	// if match_len <= dict_bytes_available, match fits entirely within external dictionary : just copy
	CMPQ CX, AX
	JBE memmove_match

	// The match stretches over the dictionary and our block
	// 1) copy what comes from the dictionary
	// AX = dict_bytes_available = copy_size
	// BX = &dict_end - copy_size
	// CX = match_len

	// memmove(to, from, len)
	MOVQ DI, 0(SP)
	MOVQ BX, 8(SP)
	MOVQ AX, 16(SP)
	// store extra stuff we want to recover
	// spill
	MOVQ DI, 24(SP)
	MOVQ SI, 32(SP)
	MOVQ CX, 40(SP)
	CALL runtime·memmove(SB)

	// restore registers
	MOVQ 16(SP), AX // copy_size
	MOVQ 24(SP), DI
	MOVQ 32(SP), SI
	MOVQ 40(SP), CX // match_len

	// recalc initial values
	MOVQ dst_base+0(FP), R8
	MOVQ R8, R11 // TODO: make these sensible numbers
	ADDQ dst_len+8(FP), R8
	MOVQ src_base+24(FP), R9
	ADDQ src_len+32(FP), R9
	MOVQ dict_base+48(FP), R14
	MOVQ dict_len+56(FP), R15
	MOVQ R8, R12
	SUBQ $32, R12
	MOVQ R9, R13
	SUBQ $16, R13

	// di+=copy_size
	ADDQ AX, DI

	// 2) copy the rest (> 0 bytes) from the start of the current block:
	// a match at offset di from &dst.
	SUBQ AX, CX
	MOVQ R11, BX
	MOVQ DI, DX
	SUBQ R11, DX
	JMP  copy_match_dispatch

memmove_match:
	// memmove(to, from, len)
	MOVQ DI, 0(SP)
	MOVQ BX, 8(SP)
	MOVQ CX, 16(SP)

	// Spill registers. Increment DI now so we don't need to save CX.
	ADDQ CX, DI
	MOVQ DI, 24(SP)
	MOVQ SI, 32(SP)

	CALL runtime·memmove(SB)

	// restore registers
	MOVQ 24(SP), DI
	MOVQ 32(SP), SI

	// recalc initial values
	MOVQ dst_base+0(FP), R8
	MOVQ R8, R11 // TODO: make these sensible numbers
	ADDQ dst_len+8(FP), R8
	MOVQ src_base+24(FP), R9
	ADDQ src_len+32(FP), R9
	MOVQ R8, R12
	SUBQ $32, R12
	MOVQ R9, R13
	SUBQ $16, R13
	MOVQ dict_base+48(FP), R14
	MOVQ dict_len+56(FP), R15
	XORL CX, CX

loopcheck:
	// for si < len(src)
	CMPQ SI, R9
	JB   loop

end:
	// Remaining length must be zero.
	TESTQ CX, CX
	JNE   err_corrupt

	SUBQ R11, DI
	MOVQ DI, ret+72(FP)
	RET

err_corrupt:
	MOVQ $-1, ret+72(FP)
	RET

err_short_buf:
	MOVQ $-2, ret+72(FP)
	RET

err_short_dict:
	MOVQ $-3, ret+72(FP)
	RET

// tileStep[offset] = (16/offset)*offset for offsets 3, 5, 6, 7, 9..15.
DATA tileStep<>+0(SB)/8, $0x0e0c0f100f101000
DATA tileStep<>+8(SB)/8, $0x0f0e0d0c0b0a0910
GLOBL tileStep<>(SB), RODATA|NOPTR, $16
