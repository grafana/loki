//go:build !purego

#include "textflag.h"
#include "unpack_neon_macros_arm64.h"

// unpackInt32x4bitNEON implements NEON unpacking for bitWidth=4 using direct bit manipulation
// Each byte contains 2 values of 4 bits each
//
// func unpackInt32x4bitNEON(dst []int32, src []byte, bitWidth uint)
TEXT ·unpackInt32x4bitNEON(SB), NOSPLIT, $0-56
	MOVD dst_base+0(FP), R0   // R0 = dst pointer
	MOVD dst_len+8(FP), R1    // R1 = dst length
	MOVD src_base+24(FP), R2  // R2 = src pointer
	MOVD bitWidth+48(FP), R3  // R3 = bitWidth (should be 4)

	MOVD $0, R5         // R5 = index (initialize early for tail path)

	// Check if we have at least 16 values to process
	CMP $16, R1
	BLT neon4_tail

	// Round down to multiple of 16 for NEON processing
	MOVD R1, R4
	LSR $4, R4, R4      // R4 = len / 16
	LSL $4, R4, R4      // R4 = aligned length (multiple of 16)

	// Load mask for 4 bits (0x0F0F0F0F...)
	MOVD $0x0F0F0F0F0F0F0F0F, R6
	VMOV R6, V31.D[0]
	VMOV R6, V31.D[1]   // V31 = mask for low nibbles

neon4_loop:
	// Load 8 bytes (contains 16 x 4-bit values)
	VLD1 (R2), [V0.B8]

	// Extract low nibbles (values at even nibble positions)
	VAND V31.B16, V0.B16, V1.B16    // V1 = low nibbles

	// Extract high nibbles (values at odd nibble positions)
	VUSHR $4, V0.B16, V2.B16        // V2 = high nibbles (shifted down)
	VAND V31.B16, V2.B16, V2.B16    // V2 = high nibbles (masked)

	// Now V1 has values [0,2,4,6,8,10,12,14] and V2 has [1,3,5,7,9,11,13,15]
	// We need to interleave them: [0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15]
	VZIP1 V2.B8, V1.B8, V3.B8       // V3 = interleaved low half
	VZIP2 V2.B8, V1.B8, V4.B8       // V4 = interleaved high half

	// Widen first 8 values (V3) to int32
	USHLL_8H_8B(5, 3)               // V5.8H ← V3.8B
	USHLL_4S_4H(6, 5)               // V6.4S ← V5.4H (values 0-3)
	USHLL2_4S_8H(7, 5)              // V7.4S ← V5.8H (values 4-7)

	// Widen second 8 values (V4) to int32
	USHLL_8H_8B(8, 4)               // V8.8H ← V4.8B
	USHLL_4S_4H(9, 8)               // V9.4S ← V8.4H (values 8-11)
	USHLL2_4S_8H(10, 8)             // V10.4S ← V8.8H (values 12-15)

	// Store 16 int32 values (64 bytes)
	VST1 [V6.S4, V7.S4], (R0)
	ADD $32, R0, R0
	VST1 [V9.S4, V10.S4], (R0)
	ADD $32, R0, R0

	// Advance pointers
	ADD $8, R2, R2       // src += 8 bytes
	ADD $16, R5, R5      // index += 16

	CMP R4, R5
	BLT neon4_loop

neon4_tail:
	// Handle remaining elements with scalar fallback
	CMP R1, R5
	BEQ neon4_done

	// Compute remaining elements
	SUB R5, R1, R1

	// Fall back to scalar unpack for tail
	MOVD $0x0F, R4      // bitMask = 0x0F (4 bits)
	MOVD $0, R6         // bitOffset = 0 (start from current R2 position)
	MOVD $0, R7         // loop counter = 0
	B neon4_scalar_test

neon4_scalar_loop:
	MOVD R6, R8
	LSR $3, R8, R8      // byte_index = bitOffset / 8
	MOVBU (R2)(R8), R9  // Load byte from current position

	MOVD R6, R10
	AND $7, R10, R10    // bit_offset = bitOffset % 8

	LSR R10, R9, R9     // Shift right by bit offset
	AND $0x0F, R9, R9   // Mask to get 4 bits
	MOVW R9, (R0)       // Store as int32

	ADD $4, R0, R0      // dst++
	ADD $4, R6, R6      // bitOffset += 4
	ADD $1, R7, R7      // counter++

neon4_scalar_test:
	CMP R1, R7
	BLT neon4_scalar_loop

neon4_done:
	RET
