//go:build !purego

#include "textflag.h"
#include "unpack_neon_macros_arm64.h"

// unpackInt32x2bitNEON implements NEON unpacking for bitWidth=2 using direct bit manipulation
// Each byte contains 4 values of 2 bits each: [bits 6-7][bits 4-5][bits 2-3][bits 0-1]
//
// func unpackInt32x2bitNEON(dst []int32, src []byte, bitWidth uint)
TEXT ·unpackInt32x2bitNEON(SB), NOSPLIT, $0-56
	MOVD dst_base+0(FP), R0   // R0 = dst pointer
	MOVD dst_len+8(FP), R1    // R1 = dst length
	MOVD src_base+24(FP), R2  // R2 = src pointer
	MOVD bitWidth+48(FP), R3  // R3 = bitWidth (should be 2)

	MOVD $0, R5         // R5 = index (initialize early for tail path)

	// Check if we have at least 32 values to process
	CMP $32, R1
	BLT neon2_tail

	// Round down to multiple of 32 for NEON processing
	MOVD R1, R4
	LSR $5, R4, R4      // R4 = len / 32
	LSL $5, R4, R4      // R4 = aligned length (multiple of 32)

	// Load mask for 2 bits (0x03030303...)
	MOVD $0x0303030303030303, R6
	VMOV R6, V31.D[0]
	VMOV R6, V31.D[1]   // V31 = mask for 2-bit values

neon2_loop:
	// Load 8 bytes (contains 32 x 2-bit values)
	VLD1 (R2), [V0.B8]

	// Extract bits [1:0] from each byte (values at positions 0,4,8,12,...)
	VAND V31.B16, V0.B16, V1.B16

	// Extract bits [3:2] from each byte (values at positions 1,5,9,13,...)
	VUSHR $2, V0.B16, V2.B16
	VAND V31.B16, V2.B16, V2.B16

	// Extract bits [5:4] from each byte (values at positions 2,6,10,14,...)
	VUSHR $4, V0.B16, V3.B16
	VAND V31.B16, V3.B16, V3.B16

	// Extract bits [7:6] from each byte (values at positions 3,7,11,15,...)
	VUSHR $6, V0.B16, V4.B16
	VAND V31.B16, V4.B16, V4.B16

	// Interleave using two stages of ZIP operations
	// Stage 1: ZIP pairs at byte level
	VZIP1 V2.B8, V1.B8, V5.B8     // V5 = [V1[0],V2[0],V1[1],V2[1],V1[2],V2[2],V1[3],V2[3]]
	VZIP1 V4.B8, V3.B8, V6.B8     // V6 = [V3[0],V4[0],V3[1],V4[1],V3[2],V4[2],V3[3],V4[3]]
	VZIP2 V2.B8, V1.B8, V7.B8     // V7 = [V1[4],V2[4],V1[5],V2[5],V1[6],V2[6],V1[7],V2[7]]
	VZIP2 V4.B8, V3.B8, V8.B8     // V8 = [V3[4],V4[4],V3[5],V4[5],V3[6],V4[6],V3[7],V4[7]]

	// Stage 2: ZIP quads at 16-bit level to get final sequential order
	VZIP1 V6.H4, V5.H4, V13.H4    // V13 = [V1[0],V2[0],V3[0],V4[0],V1[1],V2[1],V3[1],V4[1]] = values 0-7
	VZIP2 V6.H4, V5.H4, V14.H4    // V14 = [V1[2],V2[2],V3[2],V4[2],V1[3],V2[3],V3[3],V4[3]] = values 8-15
	VZIP1 V8.H4, V7.H4, V15.H4    // V15 = [V1[4],V2[4],V3[4],V4[4],V1[5],V2[5],V3[5],V4[5]] = values 16-23
	VZIP2 V8.H4, V7.H4, V16.H4    // V16 = [V1[6],V2[6],V3[6],V4[6],V1[7],V2[7],V3[7],V4[7]] = values 24-31

	// Widen first 8 values (V13) to int32
	USHLL_8H_8B(17, 13)          // V17.8H ← V13.8B
	USHLL_4S_4H(18, 17)          // V18.4S ← V17.4H (values 0-3)
	USHLL2_4S_8H(19, 17)         // V19.4S ← V17.8H (values 4-7)

	// Widen second 8 values (V14) to int32
	USHLL_8H_8B(20, 14)          // V20.8H ← V14.8B
	USHLL_4S_4H(21, 20)          // V21.4S ← V20.4H (values 8-11)
	USHLL2_4S_8H(22, 20)         // V22.4S ← V20.8H (values 12-15)

	// Widen third 8 values (V15) to int32
	USHLL_8H_8B(23, 15)          // V23.8H ← V15.8B
	USHLL_4S_4H(24, 23)          // V24.4S ← V23.4H (values 16-19)
	USHLL2_4S_8H(25, 23)         // V25.4S ← V23.8H (values 20-23)

	// Widen fourth 8 values (V16) to int32
	USHLL_8H_8B(26, 16)          // V26.8H ← V16.8B
	USHLL_4S_4H(27, 26)          // V27.4S ← V26.4H (values 24-27)
	USHLL2_4S_8H(28, 26)         // V28.4S ← V26.8H (values 28-31)

	// Store 32 int32 values (128 bytes)
	VST1 [V18.S4, V19.S4], (R0)
	ADD $32, R0, R0
	VST1 [V21.S4, V22.S4], (R0)
	ADD $32, R0, R0
	VST1 [V24.S4, V25.S4], (R0)
	ADD $32, R0, R0
	VST1 [V27.S4, V28.S4], (R0)
	ADD $32, R0, R0

	// Advance pointers
	ADD $8, R2, R2       // src += 8 bytes
	ADD $32, R5, R5      // index += 32

	CMP R4, R5
	BLT neon2_loop

neon2_tail:
	// Handle remaining elements with scalar fallback
	CMP R1, R5
	BEQ neon2_done

	// Compute remaining elements
	SUB R5, R1, R1

	// Fall back to scalar unpack for tail
	MOVD $3, R4         // bitMask = 3 (0b11 for 2 bits)
	MOVD $0, R6         // bitOffset = 0
	MOVD $0, R7         // index = 0
	B neon2_scalar_test

neon2_scalar_loop:
	MOVD R6, R8
	LSR $3, R8, R8      // byte_index = bitOffset / 8
	MOVBU (R2)(R8), R9  // Load byte

	MOVD R6, R10
	AND $7, R10, R10    // bit_offset = bitOffset % 8

	LSR R10, R9, R9     // Shift right by bit offset
	AND $3, R9, R9      // Mask to get 2 bits
	MOVW R9, (R0)       // Store as int32

	ADD $4, R0, R0      // dst++
	ADD $2, R6, R6      // bitOffset += 2
	ADD $1, R7, R7      // index++

neon2_scalar_test:
	CMP R1, R7
	BLT neon2_scalar_loop

neon2_done:
	RET
