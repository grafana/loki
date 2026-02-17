//go:build !purego

#include "textflag.h"
#include "unpack_neon_macros_arm64.h"

// unpackInt64x1bitNEON implements NEON unpacking for bitWidth=1 using direct bit manipulation
// Each byte contains 8 bits: [bit7][bit6][bit5][bit4][bit3][bit2][bit1][bit0]
//
// func unpackInt64x1bitNEON(dst []int64, src []byte, bitWidth uint)
TEXT ·unpackInt64x1bitNEON(SB), NOSPLIT, $0-56
	MOVD dst_base+0(FP), R0   // R0 = dst pointer
	MOVD dst_len+8(FP), R1    // R1 = dst length
	MOVD src_base+24(FP), R2  // R2 = src pointer
	MOVD bitWidth+48(FP), R3  // R3 = bitWidth (should be 1)

	MOVD $0, R5         // R5 = index (initialize early for tail path)

	// Check if we have at least 64 values to process
	CMP $64, R1
	BLT neon1_tail_int64

	// Round down to multiple of 64 for NEON processing
	MOVD R1, R4
	LSR $6, R4, R4      // R4 = len / 64
	LSL $6, R4, R4      // R4 = aligned length (multiple of 64)

	// Load mask for 1 bit (0x01010101...)
	MOVD $0x0101010101010101, R6
	VMOV R6, V31.D[0]
	VMOV R6, V31.D[1]   // V31 = mask for single bits

neon1_loop_int64:
	// Load 8 bytes (contains 64 x 1-bit values)
	VLD1 (R2), [V0.B8]

	// Extract each bit position (8 separate streams)
	VAND V31.B16, V0.B16, V1.B16              // V1 = bit 0

	VUSHR $1, V0.B16, V2.B16
	VAND V31.B16, V2.B16, V2.B16              // V2 = bit 1

	VUSHR $2, V0.B16, V3.B16
	VAND V31.B16, V3.B16, V3.B16              // V3 = bit 2

	VUSHR $3, V0.B16, V4.B16
	VAND V31.B16, V4.B16, V4.B16              // V4 = bit 3

	VUSHR $4, V0.B16, V5.B16
	VAND V31.B16, V5.B16, V5.B16              // V5 = bit 4

	VUSHR $5, V0.B16, V6.B16
	VAND V31.B16, V6.B16, V6.B16              // V6 = bit 5

	VUSHR $6, V0.B16, V7.B16
	VAND V31.B16, V7.B16, V7.B16              // V7 = bit 6

	VUSHR $7, V0.B16, V8.B16
	VAND V31.B16, V8.B16, V8.B16              // V8 = bit 7

	// Stage 1: ZIP pairs (8 streams → 4 streams of pairs)
	VZIP1 V2.B8, V1.B8, V9.B8                 // V9 = [bit0,bit1] interleaved
	VZIP1 V4.B8, V3.B8, V10.B8                // V10 = [bit2,bit3] interleaved
	VZIP1 V6.B8, V5.B8, V11.B8                // V11 = [bit4,bit5] interleaved
	VZIP1 V8.B8, V7.B8, V12.B8                // V12 = [bit6,bit7] interleaved

	VZIP2 V2.B8, V1.B8, V13.B8                // V13 = [bit0,bit1] upper half
	VZIP2 V4.B8, V3.B8, V14.B8                // V14 = [bit2,bit3] upper half
	VZIP2 V6.B8, V5.B8, V15.B8                // V15 = [bit4,bit5] upper half
	VZIP2 V8.B8, V7.B8, V16.B8                // V16 = [bit6,bit7] upper half

	// Stage 2: ZIP quads (4 streams → 2 streams of quads)
	VZIP1 V10.H4, V9.H4, V17.H4               // V17 = [0,1,2,3] interleaved
	VZIP1 V12.H4, V11.H4, V18.H4              // V18 = [4,5,6,7] interleaved
	VZIP2 V10.H4, V9.H4, V19.H4               // V19 = [0,1,2,3] next
	VZIP2 V12.H4, V11.H4, V20.H4              // V20 = [4,5,6,7] next

	VZIP1 V14.H4, V13.H4, V21.H4              // V21 = upper [0,1,2,3]
	VZIP1 V16.H4, V15.H4, V22.H4              // V22 = upper [4,5,6,7]
	VZIP2 V14.H4, V13.H4, V23.H4              // V23 = upper [0,1,2,3] next
	VZIP2 V16.H4, V15.H4, V24.H4              // V24 = upper [4,5,6,7] next

	// Stage 3: ZIP octets (2 streams → fully sequential)
	VZIP1 V18.S2, V17.S2, V25.S2              // V25 = values 0-7
	VZIP2 V18.S2, V17.S2, V26.S2              // V26 = values 8-15
	VZIP1 V20.S2, V19.S2, V27.S2              // V27 = values 16-23
	VZIP2 V20.S2, V19.S2, V28.S2              // V28 = values 24-31
	VZIP1 V22.S2, V21.S2, V1.S2               // V1 = values 32-39
	VZIP2 V22.S2, V21.S2, V2.S2               // V2 = values 40-47
	VZIP1 V24.S2, V23.S2, V3.S2               // V3 = values 48-55
	VZIP2 V24.S2, V23.S2, V4.S2               // V4 = values 56-63

	// Widen to int64 and store - each group of 8 values
	// Values 0-7
	USHLL_8H_8B(5, 25)
	USHLL_4S_4H(6, 5)
	USHLL2_4S_8H(7, 5)
	USHLL_2D_2S(8, 6)
	USHLL2_2D_4S(9, 6)
	USHLL_2D_2S(10, 7)
	USHLL2_2D_4S(11, 7)
	VST1 [V8.D2, V9.D2], (R0)
	ADD $32, R0, R0
	VST1 [V10.D2, V11.D2], (R0)
	ADD $32, R0, R0

	// Values 8-15
	USHLL_8H_8B(5, 26)
	USHLL_4S_4H(6, 5)
	USHLL2_4S_8H(7, 5)
	USHLL_2D_2S(8, 6)
	USHLL2_2D_4S(9, 6)
	USHLL_2D_2S(10, 7)
	USHLL2_2D_4S(11, 7)
	VST1 [V8.D2, V9.D2], (R0)
	ADD $32, R0, R0
	VST1 [V10.D2, V11.D2], (R0)
	ADD $32, R0, R0

	// Values 16-23
	USHLL_8H_8B(5, 27)
	USHLL_4S_4H(6, 5)
	USHLL2_4S_8H(7, 5)
	USHLL_2D_2S(8, 6)
	USHLL2_2D_4S(9, 6)
	USHLL_2D_2S(10, 7)
	USHLL2_2D_4S(11, 7)
	VST1 [V8.D2, V9.D2], (R0)
	ADD $32, R0, R0
	VST1 [V10.D2, V11.D2], (R0)
	ADD $32, R0, R0

	// Values 24-31
	USHLL_8H_8B(5, 28)
	USHLL_4S_4H(6, 5)
	USHLL2_4S_8H(7, 5)
	USHLL_2D_2S(8, 6)
	USHLL2_2D_4S(9, 6)
	USHLL_2D_2S(10, 7)
	USHLL2_2D_4S(11, 7)
	VST1 [V8.D2, V9.D2], (R0)
	ADD $32, R0, R0
	VST1 [V10.D2, V11.D2], (R0)
	ADD $32, R0, R0

	// Values 32-39
	USHLL_8H_8B(5, 1)
	USHLL_4S_4H(6, 5)
	USHLL2_4S_8H(7, 5)
	USHLL_2D_2S(8, 6)
	USHLL2_2D_4S(9, 6)
	USHLL_2D_2S(10, 7)
	USHLL2_2D_4S(11, 7)
	VST1 [V8.D2, V9.D2], (R0)
	ADD $32, R0, R0
	VST1 [V10.D2, V11.D2], (R0)
	ADD $32, R0, R0

	// Values 40-47
	USHLL_8H_8B(5, 2)
	USHLL_4S_4H(6, 5)
	USHLL2_4S_8H(7, 5)
	USHLL_2D_2S(8, 6)
	USHLL2_2D_4S(9, 6)
	USHLL_2D_2S(10, 7)
	USHLL2_2D_4S(11, 7)
	VST1 [V8.D2, V9.D2], (R0)
	ADD $32, R0, R0
	VST1 [V10.D2, V11.D2], (R0)
	ADD $32, R0, R0

	// Values 48-55
	USHLL_8H_8B(5, 3)
	USHLL_4S_4H(6, 5)
	USHLL2_4S_8H(7, 5)
	USHLL_2D_2S(8, 6)
	USHLL2_2D_4S(9, 6)
	USHLL_2D_2S(10, 7)
	USHLL2_2D_4S(11, 7)
	VST1 [V8.D2, V9.D2], (R0)
	ADD $32, R0, R0
	VST1 [V10.D2, V11.D2], (R0)
	ADD $32, R0, R0

	// Values 56-63
	USHLL_8H_8B(5, 4)
	USHLL_4S_4H(6, 5)
	USHLL2_4S_8H(7, 5)
	USHLL_2D_2S(8, 6)
	USHLL2_2D_4S(9, 6)
	USHLL_2D_2S(10, 7)
	USHLL2_2D_4S(11, 7)
	VST1 [V8.D2, V9.D2], (R0)
	ADD $32, R0, R0
	VST1 [V10.D2, V11.D2], (R0)
	ADD $32, R0, R0

	// Advance pointers
	ADD $8, R2, R2       // src += 8 bytes
	ADD $64, R5, R5      // index += 64

	CMP R4, R5
	BLT neon1_loop_int64

neon1_tail_int64:
	// Handle remaining elements with scalar fallback
	CMP R1, R5
	BEQ neon1_done_int64

	// Compute remaining elements
	SUB R5, R1, R1

	// Fall back to scalar unpack for tail
	MOVD $1, R4         // bitMask = 1
	MOVD $0, R6         // bitOffset = 0
	MOVD $0, R7         // index = 0
	B neon1_scalar_test_int64

neon1_scalar_loop_int64:
	MOVD R6, R8
	LSR $3, R8, R8      // byte_index = bitOffset / 8
	MOVBU (R2)(R8), R9  // Load byte

	MOVD R6, R10
	AND $7, R10, R10    // bit_offset = bitOffset % 8

	LSR R10, R9, R9     // Shift right by bit offset
	AND $1, R9, R9      // Mask to get bit
	MOVD R9, (R0)       // Store as int64

	ADD $8, R0, R0      // dst++
	ADD $1, R6, R6      // bitOffset++
	ADD $1, R7, R7      // index++

neon1_scalar_test_int64:
	CMP R1, R7
	BLT neon1_scalar_loop_int64

neon1_done_int64:
	RET
