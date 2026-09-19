//go:build !purego

#include "funcdata.h"
#include "textflag.h"

// func unpackInt32Default(dst []int32, src []byte, bitWidth uint)
TEXT ·unpackInt32Default(SB), NOSPLIT, $0-56
	MOVD dst_base+0(FP), R0   // R0 = dst pointer
	MOVD dst_len+8(FP), R1    // R1 = dst length
	MOVD src_base+24(FP), R2  // R2 = src pointer
	MOVD bitWidth+48(FP), R3  // R3 = bitWidth

	MOVD $1, R4               // R4 = bitMask = (1 << bitWidth) - 1
	LSL R3, R4, R4
	SUB $1, R4, R4

	MOVD $0, R5               // R5 = bitOffset
	MOVD $0, R6               // R6 = index
	B test

loop:
	MOVD R5, R7               // R7 = i = bitOffset / 32
	LSR $5, R7, R7

	MOVD R5, R8               // R8 = j = bitOffset % 32
	AND $31, R8, R8

	LSL $2, R7, R16           // R16 = i * 4
	MOVWU (R2)(R16), R9       // R9 = src[i]
	MOVW R4, R10              // R10 = bitMask
	LSL R8, R10, R10          // R10 = bitMask << j
	AND R10, R9, R9           // R9 = src[i] & (bitMask << j)
	LSR R8, R9, R9            // R9 = d = (src[i] & (bitMask << j)) >> j

	ADD R3, R8, R11           // R11 = j + bitWidth
	CMP $32, R11
	BLE next                  // if j+bitWidth <= 32, skip to next

	ADD $1, R7, R12           // R12 = i + 1
	LSL $2, R12, R16          // R16 = (i + 1) * 4
	MOVWU (R2)(R16), R13      // R13 = src[i+1]

	MOVD $32, R14             // R14 = k = 32 - j
	SUB R8, R14, R14

	MOVW R4, R15              // R15 = bitMask
	LSR R14, R15, R15         // R15 = bitMask >> k
	AND R15, R13, R13         // R13 = src[i+1] & (bitMask >> k)
	LSL R14, R13, R13         // R13 = (src[i+1] & (bitMask >> k)) << k
	ORR R13, R9, R9           // R9 = d | c

next:
	LSL $2, R6, R16           // R16 = index * 4
	MOVW R9, (R0)(R16)        // dst[index] = d
	ADD R3, R5, R5            // bitOffset += bitWidth
	ADD $1, R6, R6            // index++

test:
	CMP R1, R6
	BNE loop
	RET

// unpackInt32x1to16bitsARM64 implements optimized unpacking for bit widths 1-16
// Uses optimized scalar ARM64 operations with batched processing
//
// func unpackInt32x1to16bitsARM64(dst []int32, src []byte, bitWidth uint)
TEXT ·unpackInt32x1to16bitsARM64(SB), NOSPLIT, $0-56
	MOVD dst_base+0(FP), R0   // R0 = dst pointer
	MOVD dst_len+8(FP), R1    // R1 = dst length
	MOVD src_base+24(FP), R2  // R2 = src pointer
	MOVD bitWidth+48(FP), R3  // R3 = bitWidth

	// Check if we have at least 4 values to process
	CMP $4, R1
	BLT scalar_fallback

	// Determine which NEON path to use based on bitWidth
	CMP $1, R3
	BEQ neon_1bit
	CMP $2, R3
	BEQ neon_2bit
	CMP $3, R3
	BEQ neon_3bit
	CMP $4, R3
	BEQ neon_4bit
	CMP $5, R3
	BEQ neon_5bit
	CMP $6, R3
	BEQ neon_6bit
	CMP $7, R3
	BEQ neon_7bit
	CMP $8, R3
	BEQ neon_8bit
	CMP $16, R3
	BEQ neon_16bit

	// For other bit widths, fall back to scalar
	B scalar_fallback

neon_1bit:
	// BitWidth 1: 8 int32 values packed in 1 byte
	// Process 8 values at a time using scalar operations

	// Round down to multiple of 8 for processing
	MOVD R1, R4
	LSR $3, R4, R4      // R4 = len / 8
	LSL $3, R4, R4      // R4 = aligned length (multiple of 8)

	MOVD $0, R5         // R5 = index
	CMP $0, R4
	BEQ scalar_fallback

neon_1bit_loop:
	// Load 1 byte (contains 8 values, 1 bit each)
	MOVBU (R2), R6

	// Extract 8 values manually (bits 0-7)
	// Value 0: bit 0
	AND $1, R6, R7
	MOVW R7, (R0)

	// Value 1: bit 1
	LSR $1, R6, R7
	AND $1, R7, R7
	MOVW R7, 4(R0)

	// Value 2: bit 2
	LSR $2, R6, R7
	AND $1, R7, R7
	MOVW R7, 8(R0)

	// Value 3: bit 3
	LSR $3, R6, R7
	AND $1, R7, R7
	MOVW R7, 12(R0)

	// Value 4: bit 4
	LSR $4, R6, R7
	AND $1, R7, R7
	MOVW R7, 16(R0)

	// Value 5: bit 5
	LSR $5, R6, R7
	AND $1, R7, R7
	MOVW R7, 20(R0)

	// Value 6: bit 6
	LSR $6, R6, R7
	AND $1, R7, R7
	MOVW R7, 24(R0)

	// Value 7: bit 7
	LSR $7, R6, R7
	AND $1, R7, R7
	MOVW R7, 28(R0)

	// Advance pointers
	ADD $1, R2, R2      // src += 1 byte (8 values)
	ADD $32, R0, R0     // dst += 8 int32 (32 bytes)
	ADD $8, R5, R5      // index += 8

	CMP R4, R5
	BLT neon_1bit_loop

	CMP R1, R5
	BEQ neon_done
	SUB R5, R1, R1
	B scalar_fallback_entry

neon_2bit:
	// BitWidth 2: 4 int32 values packed in 1 byte
	// Process 4 values at a time using scalar operations

	MOVD R1, R4
	LSR $2, R4, R4      // R4 = len / 4
	LSL $2, R4, R4      // R4 = aligned length (multiple of 4)

	MOVD $0, R5
	CMP $0, R4
	BEQ scalar_fallback

neon_2bit_loop:
	// Load 1 byte (contains 4 values, 2 bits each)
	MOVBU (R2), R6

	// Extract 4 values manually (bits 0-1, 2-3, 4-5, 6-7)
	// Value 0: bits 0-1
	AND $3, R6, R7
	MOVW R7, (R0)

	// Value 1: bits 2-3
	LSR $2, R6, R7
	AND $3, R7, R7
	MOVW R7, 4(R0)

	// Value 2: bits 4-5
	LSR $4, R6, R7
	AND $3, R7, R7
	MOVW R7, 8(R0)

	// Value 3: bits 6-7
	LSR $6, R6, R7
	AND $3, R7, R7
	MOVW R7, 12(R0)

	// Advance pointers
	ADD $1, R2, R2      // src += 1 byte (4 values)
	ADD $16, R0, R0     // dst += 4 int32 (16 bytes)
	ADD $4, R5, R5      // index += 4

	CMP R4, R5
	BLT neon_2bit_loop

	CMP R1, R5
	BEQ neon_done
	SUB R5, R1, R1
	B scalar_fallback_entry

neon_3bit:
	// BitWidth 3: 8 int32 values packed in 3 bytes
	// Process 8 values at a time using scalar operations

	// Round down to multiple of 8 for processing
	MOVD R1, R4
	LSR $3, R4, R4      // R4 = len / 8
	LSL $3, R4, R4      // R4 = aligned length (multiple of 8)

	MOVD $0, R5
	CMP $0, R4
	BEQ scalar_fallback

neon_3bit_loop:
	// Load 3 bytes as 32-bit value (4th byte will be ignored)
	// Bytes 0-2 contain: [val7:val6:val5:val4:val3:val2:val1:val0]
	// Bits layout: [23:21][20:18][17:15][14:12][11:9][8:6][5:3][2:0]
	MOVWU (R2), R6

	// Value 0: bits 0-2
	AND $7, R6, R7
	MOVW R7, (R0)

	// Value 1: bits 3-5
	LSR $3, R6, R7
	AND $7, R7, R7
	MOVW R7, 4(R0)

	// Value 2: bits 6-8
	LSR $6, R6, R7
	AND $7, R7, R7
	MOVW R7, 8(R0)

	// Value 3: bits 9-11
	LSR $9, R6, R7
	AND $7, R7, R7
	MOVW R7, 12(R0)

	// Value 4: bits 12-14
	LSR $12, R6, R7
	AND $7, R7, R7
	MOVW R7, 16(R0)

	// Value 5: bits 15-17
	LSR $15, R6, R7
	AND $7, R7, R7
	MOVW R7, 20(R0)

	// Value 6: bits 18-20
	LSR $18, R6, R7
	AND $7, R7, R7
	MOVW R7, 24(R0)

	// Value 7: bits 21-23
	LSR $21, R6, R7
	AND $7, R7, R7
	MOVW R7, 28(R0)

	// Advance pointers
	ADD $3, R2, R2      // src += 3 bytes (8 values)
	ADD $32, R0, R0     // dst += 8 int32 (32 bytes)
	ADD $8, R5, R5      // index += 8

	CMP R4, R5
	BLT neon_3bit_loop

	CMP R1, R5
	BEQ neon_done
	SUB R5, R1, R1
	B scalar_fallback_entry

neon_4bit:
	// BitWidth 4: 4 int32 values packed in 2 bytes
	// Process 4 values at a time using scalar operations

	MOVD R1, R4
	LSR $2, R4, R4      // R4 = len / 4
	LSL $2, R4, R4      // R4 = aligned length (multiple of 4)

	MOVD $0, R5
	CMP $0, R4
	BEQ scalar_fallback

neon_4bit_loop:
	// Load 2 bytes (contains 4 values, 4 bits each)
	MOVHU (R2), R6

	// Extract 4 values manually (nibbles)
	// Value 0: bits 0-3
	AND $15, R6, R7
	MOVW R7, (R0)

	// Value 1: bits 4-7
	LSR $4, R6, R7
	AND $15, R7, R7
	MOVW R7, 4(R0)

	// Value 2: bits 8-11
	LSR $8, R6, R7
	AND $15, R7, R7
	MOVW R7, 8(R0)

	// Value 3: bits 12-15
	LSR $12, R6, R7
	AND $15, R7, R7
	MOVW R7, 12(R0)

	// Advance pointers
	ADD $2, R2, R2      // src += 2 bytes (4 values)
	ADD $16, R0, R0     // dst += 4 int32 (16 bytes)
	ADD $4, R5, R5      // index += 4

	CMP R4, R5
	BLT neon_4bit_loop

	CMP R1, R5
	BEQ neon_done
	SUB R5, R1, R1
	B scalar_fallback_entry

neon_5bit:
	// BitWidth 5: 8 int32 values packed in 5 bytes
	// Process 8 values at a time using scalar operations

	// Round down to multiple of 8 for processing
	MOVD R1, R4
	LSR $3, R4, R4      // R4 = len / 8
	LSL $3, R4, R4      // R4 = aligned length (multiple of 8)

	MOVD $0, R5
	CMP $0, R4
	BEQ scalar_fallback

neon_5bit_loop:
	// Load 5 bytes as 64-bit value (upper bytes will be ignored)
	// 8 values × 5 bits = 40 bits = 5 bytes
	// Bits layout: [39:35][34:30][29:25][24:20][19:15][14:10][9:5][4:0]
	MOVD (R2), R6

	// Value 0: bits 0-4
	AND $31, R6, R7
	MOVW R7, (R0)

	// Value 1: bits 5-9
	LSR $5, R6, R7
	AND $31, R7, R7
	MOVW R7, 4(R0)

	// Value 2: bits 10-14
	LSR $10, R6, R7
	AND $31, R7, R7
	MOVW R7, 8(R0)

	// Value 3: bits 15-19
	LSR $15, R6, R7
	AND $31, R7, R7
	MOVW R7, 12(R0)

	// Value 4: bits 20-24
	LSR $20, R6, R7
	AND $31, R7, R7
	MOVW R7, 16(R0)

	// Value 5: bits 25-29
	LSR $25, R6, R7
	AND $31, R7, R7
	MOVW R7, 20(R0)

	// Value 6: bits 30-34
	LSR $30, R6, R7
	AND $31, R7, R7
	MOVW R7, 24(R0)

	// Value 7: bits 35-39
	LSR $35, R6, R7
	AND $31, R7, R7
	MOVW R7, 28(R0)

	// Advance pointers
	ADD $5, R2, R2      // src += 5 bytes (8 values)
	ADD $32, R0, R0     // dst += 8 int32 (32 bytes)
	ADD $8, R5, R5      // index += 8

	CMP R4, R5
	BLT neon_5bit_loop

	CMP R1, R5
	BEQ neon_done
	SUB R5, R1, R1
	B scalar_fallback_entry

neon_6bit:
	// BitWidth 6: 4 int32 values packed in 3 bytes
	// Process 4 values at a time using scalar operations

	MOVD R1, R4
	LSR $2, R4, R4      // R4 = len / 4
	LSL $2, R4, R4      // R4 = aligned length (multiple of 4)

	MOVD $0, R5
	CMP $0, R4
	BEQ scalar_fallback

neon_6bit_loop:
	// Load 3 bytes as 32-bit value (4th byte will be ignored)
	// 4 values × 6 bits = 24 bits = 3 bytes
	// Bits layout: [23:18][17:12][11:6][5:0]
	MOVWU (R2), R6

	// Value 0: bits 0-5
	AND $63, R6, R7
	MOVW R7, (R0)

	// Value 1: bits 6-11
	LSR $6, R6, R7
	AND $63, R7, R7
	MOVW R7, 4(R0)

	// Value 2: bits 12-17
	LSR $12, R6, R7
	AND $63, R7, R7
	MOVW R7, 8(R0)

	// Value 3: bits 18-23
	LSR $18, R6, R7
	AND $63, R7, R7
	MOVW R7, 12(R0)

	// Advance pointers
	ADD $3, R2, R2      // src += 3 bytes (4 values)
	ADD $16, R0, R0     // dst += 4 int32 (16 bytes)
	ADD $4, R5, R5      // index += 4

	CMP R4, R5
	BLT neon_6bit_loop

	CMP R1, R5
	BEQ neon_done
	SUB R5, R1, R1
	B scalar_fallback_entry

neon_7bit:
	// BitWidth 7: 8 int32 values packed in 7 bytes
	// Process 8 values at a time using scalar operations

	// Round down to multiple of 8 for processing
	MOVD R1, R4
	LSR $3, R4, R4      // R4 = len / 8
	LSL $3, R4, R4      // R4 = aligned length (multiple of 8)

	MOVD $0, R5
	CMP $0, R4
	BEQ scalar_fallback

neon_7bit_loop:
	// Load 7 bytes as 64-bit value (8th byte will be ignored)
	// 8 values × 7 bits = 56 bits = 7 bytes
	// Bits layout: [55:49][48:42][41:35][34:28][27:21][20:14][13:7][6:0]
	MOVD (R2), R6

	// Value 0: bits 0-6
	AND $127, R6, R7
	MOVW R7, (R0)

	// Value 1: bits 7-13
	LSR $7, R6, R7
	AND $127, R7, R7
	MOVW R7, 4(R0)

	// Value 2: bits 14-20
	LSR $14, R6, R7
	AND $127, R7, R7
	MOVW R7, 8(R0)

	// Value 3: bits 21-27
	LSR $21, R6, R7
	AND $127, R7, R7
	MOVW R7, 12(R0)

	// Value 4: bits 28-34
	LSR $28, R6, R7
	AND $127, R7, R7
	MOVW R7, 16(R0)

	// Value 5: bits 35-41
	LSR $35, R6, R7
	AND $127, R7, R7
	MOVW R7, 20(R0)

	// Value 6: bits 42-48
	LSR $42, R6, R7
	AND $127, R7, R7
	MOVW R7, 24(R0)

	// Value 7: bits 49-55
	LSR $49, R6, R7
	AND $127, R7, R7
	MOVW R7, 28(R0)

	// Advance pointers
	ADD $7, R2, R2      // src += 7 bytes (8 values)
	ADD $32, R0, R0     // dst += 8 int32 (32 bytes)
	ADD $8, R5, R5      // index += 8

	CMP R4, R5
	BLT neon_7bit_loop

	CMP R1, R5
	BEQ neon_done
	SUB R5, R1, R1
	B scalar_fallback_entry

neon_8bit:
	// BitWidth 8: 4 int32 values packed in 4 bytes
	// Process 4 values at a time using NEON

	// Calculate how many full groups of 4 we can process
	MOVD R1, R4
	LSR $2, R4, R4      // R4 = len / 4
	LSL $2, R4, R4      // R4 = (len / 4) * 4 = aligned length

	MOVD $0, R5         // R5 = index
	CMP $0, R4
	BEQ scalar_fallback

neon_8bit_loop:
	// Load 4 bytes as 4 uint8 values into lower part of V0
	// We need to load bytes and zero-extend to 32-bit

	// Load 4 bytes to W6
	MOVWU (R2), R6

	// Extract bytes and write as int32
	// Byte 0
	AND $0xFF, R6, R7
	MOVW R7, (R0)

	// Byte 1
	LSR $8, R6, R7
	AND $0xFF, R7, R7
	MOVW R7, 4(R0)

	// Byte 2
	LSR $16, R6, R7
	AND $0xFF, R7, R7
	MOVW R7, 8(R0)

	// Byte 3
	LSR $24, R6, R7
	MOVW R7, 12(R0)

	// Advance pointers
	ADD $4, R2, R2      // src += 4 bytes
	ADD $16, R0, R0     // dst += 4 int32 (16 bytes)
	ADD $4, R5, R5      // index += 4

	CMP R4, R5
	BLT neon_8bit_loop

	// Handle tail with scalar
	CMP R1, R5
	BEQ neon_done

	// Calculate remaining elements
	SUB R5, R1, R1      // R1 = remaining elements
	B scalar_fallback_entry

neon_16bit:
	// BitWidth 16: 4 int32 values packed in 8 bytes
	// Process 4 values at a time

	MOVD R1, R4
	LSR $2, R4, R4      // R4 = len / 4
	LSL $2, R4, R4      // R4 = (len / 4) * 4

	MOVD $0, R5         // R5 = index
	CMP $0, R4
	BEQ scalar_fallback

neon_16bit_loop:
	// Load 8 bytes as 4 uint16 values
	MOVD (R2), R6       // Load 8 bytes into R6

	// Extract 16-bit values and write as int32
	// Value 0 (bits 0-15)
	AND $0xFFFF, R6, R7
	MOVW R7, (R0)

	// Value 1 (bits 16-31)
	LSR $16, R6, R7
	AND $0xFFFF, R7, R7
	MOVW R7, 4(R0)

	// Value 2 (bits 32-47)
	LSR $32, R6, R7
	AND $0xFFFF, R7, R7
	MOVW R7, 8(R0)

	// Value 3 (bits 48-63)
	LSR $48, R6, R7
	MOVW R7, 12(R0)

	// Advance pointers
	ADD $8, R2, R2      // src += 8 bytes
	ADD $16, R0, R0     // dst += 4 int32 (16 bytes)
	ADD $4, R5, R5      // index += 4

	CMP R4, R5
	BLT neon_16bit_loop

	// Handle tail with scalar
	CMP R1, R5
	BEQ neon_done

	SUB R5, R1, R1
	B scalar_fallback_entry

neon_done:
	RET

scalar_fallback:
	MOVD $0, R5         // Start from beginning
	// R0, R1, R2, R3 already set from function args

scalar_fallback_entry:
	// R0 = current dst position (already advanced)
	// R1 = remaining elements
	// R2 = current src position (already advanced)
	// R3 = bitWidth
	// R5 = elements already processed

	// Fall back to scalar implementation for remaining elements
	CMP $0, R1
	BEQ scalar_done     // No remaining elements

	MOVD $1, R4         // R4 = bitMask = (1 << bitWidth) - 1
	LSL R3, R4, R4
	SUB $1, R4, R4

	// bitOffset starts from 0 relative to current R2 position
	// (not total offset, since R2 is already advanced)
	MOVD $0, R6         // R6 = bitOffset (relative to current R2)
	MOVD $0, R7         // R7 = index (within remaining elements)
	B scalar_test

scalar_loop:
	MOVD R6, R8         // R8 = i = bitOffset / 32
	LSR $5, R8, R8

	MOVD R6, R9         // R9 = j = bitOffset % 32
	AND $31, R9, R9

	LSL $2, R8, R10     // R10 = i * 4
	MOVWU (R2)(R10), R11  // R11 = src[i] (relative to current R2)
	MOVW R4, R12        // R12 = bitMask
	LSL R9, R12, R12    // R12 = bitMask << j
	AND R12, R11, R11   // R11 = src[i] & (bitMask << j)
	LSR R9, R11, R11    // R11 = d = (src[i] & (bitMask << j)) >> j

	ADD R3, R9, R12     // R12 = j + bitWidth
	CMP $32, R12
	BLE scalar_next     // if j+bitWidth <= 32, skip to next

	ADD $1, R8, R13     // R13 = i + 1
	LSL $2, R13, R10    // R10 = (i + 1) * 4
	MOVWU (R2)(R10), R14  // R14 = src[i+1]

	MOVD $32, R15       // R15 = k = 32 - j
	SUB R9, R15, R15

	MOVW R4, R16        // R16 = bitMask
	LSR R15, R16, R16   // R16 = bitMask >> k
	AND R16, R14, R14   // R14 = src[i+1] & (bitMask >> k)
	LSL R15, R14, R14   // R14 = (src[i+1] & (bitMask >> k)) << k
	ORR R14, R11, R11   // R11 = d | c

scalar_next:
	LSL $2, R7, R10     // R10 = index * 4
	MOVW R11, (R0)(R10) // dst[index] = d (relative to current R0)
	ADD R3, R6, R6      // bitOffset += bitWidth
	ADD $1, R7, R7      // index++

scalar_test:
	CMP R1, R7
	BLT scalar_loop

scalar_done:
	RET

// Macro definitions for unsupported NEON instructions using WORD encodings
// USHLL Vd.8H, Vn.8B, #0 - widen 8x8-bit to 8x16-bit
#define USHLL_8H_8B(vd, vn) WORD $(0x2f08a400 | (vd) | ((vn)<<5))

// USHLL2 Vd.8H, Vn.16B, #0 - widen upper 8x8-bit to 8x16-bit
#define USHLL2_8H_16B(vd, vn) WORD $(0x6f08a400 | (vd) | ((vn)<<5))

// USHLL Vd.4S, Vn.4H, #0 - widen 4x16-bit to 4x32-bit
#define USHLL_4S_4H(vd, vn) WORD $(0x2f10a400 | (vd) | ((vn)<<5))

// USHLL2 Vd.4S, Vn.8H, #0 - widen upper 4x16-bit to 4x32-bit
#define USHLL2_4S_8H(vd, vn) WORD $(0x6f10a400 | (vd) | ((vn)<<5))

// USHLL Vd.2D, Vn.2S, #0 - widen 2x32-bit to 2x64-bit
#define USHLL_2D_2S(vd, vn) WORD $(0x2f20a400 | (vd) | ((vn)<<5))

// USHLL2 Vd.2D, Vn.4S, #0 - widen upper 2x32-bit to 2x64-bit
#define USHLL2_2D_4S(vd, vn) WORD $(0x6f20a400 | (vd) | ((vn)<<5))

// Bit expansion lookup table defined in bitexpand_table_arm64.s

// unpackInt32x1bitNEON implements table-based NEON unpacking for bitWidth=1
// Uses lookup tables for parallel bit expansion
//
// func unpackInt32x1bitNEON(dst []int32, src []byte, bitWidth uint)
