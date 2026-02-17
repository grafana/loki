//go:build !purego

#include "textflag.h"
#include "unpack_neon_macros_arm64.h"

// unpackInt64x8bitNEON implements NEON unpacking for bitWidth=8
// Each byte is already a complete value - just widen to int64
// Processes 8 values at a time using NEON
//
// func unpackInt64x8bitNEON(dst []int64, src []byte, bitWidth uint)
TEXT ·unpackInt64x8bitNEON(SB), NOSPLIT, $0-56
	MOVD dst_base+0(FP), R0   // R0 = dst pointer
	MOVD dst_len+8(FP), R1    // R1 = dst length
	MOVD src_base+24(FP), R2  // R2 = src pointer
	MOVD bitWidth+48(FP), R3  // R3 = bitWidth (should be 8)

	MOVD $0, R5         // R5 = index

	// Check if we have at least 8 values to process
	CMP $8, R1
	BLT tbl8_tail

	// Round down to multiple of 8 for NEON processing
	MOVD R1, R4
	LSR $3, R4, R4      // R4 = len / 8
	LSL $3, R4, R4      // R4 = aligned length (multiple of 8)

tbl8_loop:
	// Load 8 bytes (8 x 8-bit values)
	VLD1 (R2), [V0.B8]

	// Widen to int64: byte → short → int → long
	USHLL_8H_8B(1, 0)       // V1.8H ← V0.8B (8x8-bit → 8x16-bit)
	USHLL_4S_4H(2, 1)       // V2.4S ← V1.4H (lower 4x16-bit → 4x32-bit)
	USHLL2_4S_8H(3, 1)      // V3.4S ← V1.8H (upper 4x16-bit → 4x32-bit)
	USHLL_2D_2S(4, 2)       // V4.2D ← V2.2S (lower 2x32-bit → 2x64-bit)
	USHLL2_2D_4S(5, 2)      // V5.2D ← V2.4S (upper 2x32-bit → 2x64-bit)
	USHLL_2D_2S(6, 3)       // V6.2D ← V3.2S (lower 2x32-bit → 2x64-bit)
	USHLL2_2D_4S(7, 3)      // V7.2D ← V3.4S (upper 2x32-bit → 2x64-bit)

	// Store 8 int64 values (64 bytes)
	VST1 [V4.D2, V5.D2], (R0)
	ADD $32, R0, R11    // Temporary pointer for second store
	VST1 [V6.D2, V7.D2], (R11)

	// Advance pointers
	ADD $8, R2, R2       // src += 8 bytes
	ADD $64, R0, R0      // dst += 8 int64 (64 bytes)
	ADD $8, R5, R5       // index += 8

	CMP R4, R5
	BLT tbl8_loop

tbl8_tail:
	// Handle remaining elements (0-7) one by one
	CMP R1, R5
	BGE tbl8_done

tbl8_tail_loop:
	MOVBU (R2), R6       // Load byte
	MOVD R6, (R0)        // Store as int64 (zero-extended)

	ADD $1, R2, R2       // src++
	ADD $8, R0, R0       // dst++
	ADD $1, R5, R5       // index++

	CMP R1, R5
	BLT tbl8_tail_loop

tbl8_done:
	RET
