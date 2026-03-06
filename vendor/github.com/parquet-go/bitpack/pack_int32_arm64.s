//go:build !purego

#include "funcdata.h"
#include "textflag.h"

// func packInt32ARM64(dst []byte, src []int32, bitWidth uint)
TEXT ·packInt32ARM64(SB), NOSPLIT, $0-56
	MOVD dst_base+0(FP), R0   // R0 = dst pointer
	MOVD src_base+24(FP), R1  // R1 = src pointer
	MOVD src_len+32(FP), R2   // R2 = src length
	MOVD bitWidth+48(FP), R3  // R3 = bitWidth

	// Handle bitWidth == 0
	CBZ R3, done

	// R4 = bitMask = (1 << bitWidth) - 1
	MOVD $1, R4
	LSL R3, R4, R4
	SUB $1, R4, R4

	// R5 = buffer (64-bit accumulator)
	// R6 = bufferedBits
	// R7 = byteIndex
	// R8 = loop counter (src index)
	MOVD $0, R5
	MOVD $0, R6
	MOVD $0, R7
	MOVD $0, R8

	// Main loop: process each value from src
loop:
	CMP R2, R8
	BEQ flush_remaining

	// Load value from src[R8]
	LSL $2, R8, R16          // R16 = R8 * 4
	MOVWU (R1)(R16), R9      // R9 = src[R8]

	// Mask the value: R9 = value & bitMask
	AND R4, R9, R9

	// Add to buffer: buffer |= (value << bufferedBits)
	LSL R6, R9, R10          // R10 = value << bufferedBits
	ORR R10, R5, R5          // buffer |= R10

	// bufferedBits += bitWidth
	ADD R3, R6, R6

	// Increment source index
	ADD $1, R8, R8

flush_loop:
	// While bufferedBits >= 32, flush 32-bit words
	CMP $32, R6
	BLT loop

	// Write 32-bit word to dst[byteIndex]
	MOVW R5, (R0)(R7)

	// buffer >>= 32
	LSR $32, R5, R5

	// bufferedBits -= 32
	SUB $32, R6, R6

	// byteIndex += 4
	ADD $4, R7, R7

	B flush_loop

flush_remaining:
	// If no bits remaining, we're done
	CBZ R6, done

	// Calculate remaining bytes = (bufferedBits + 7) / 8
	ADD $7, R6, R11
	LSR $3, R11, R11         // R11 = remainingBytes

	MOVD $0, R12             // R12 = i (byte counter)

flush_byte_loop:
	CMP R11, R12
	BEQ done

	// dst[byteIndex] = byte(buffer)
	MOVB R5, (R0)(R7)

	// buffer >>= 8
	LSR $8, R5, R5

	// byteIndex++, i++
	ADD $1, R7, R7
	ADD $1, R12, R12

	B flush_byte_loop

done:
	RET

// func packInt32NEON(dst []byte, src []int32, bitWidth uint)
TEXT ·packInt32NEON(SB), NOSPLIT, $0-56
	MOVD dst_base+0(FP), R0   // R0 = dst pointer
	MOVD src_base+24(FP), R1  // R1 = src pointer
	MOVD src_len+32(FP), R2   // R2 = src length
	MOVD bitWidth+48(FP), R3  // R3 = bitWidth

	// Handle bitWidth == 0
	CBZ R3, neon_done

	// Initialize processed count to 0
	MOVD $0, R5

	// Check if we have at least 4 values to process with NEON paths
	CMP $4, R2
	BLT neon_done  // Not enough values, return and let Go wrapper handle it

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

	// For other bit widths, return without processing
	// The Go wrapper will call the scalar version
	RET

neon_1bit:
	// BitWidth 1: Pack 8 int32 values into 1 byte
	MOVD R2, R4
	LSR $3, R4, R4      // R4 = len / 8
	LSL $3, R4, R4      // R4 = aligned length
	MOVD $0, R5         // R5 = index
	CMP $0, R4
	BEQ neon_done

neon_1bit_loop:
	MOVWU (R1), R6
	AND $1, R6, R6
	MOVWU 4(R1), R7
	AND $1, R7, R7
	ORR R7<<1, R6, R6
	MOVWU 8(R1), R7
	AND $1, R7, R7
	ORR R7<<2, R6, R6
	MOVWU 12(R1), R7
	AND $1, R7, R7
	ORR R7<<3, R6, R6
	MOVWU 16(R1), R7
	AND $1, R7, R7
	ORR R7<<4, R6, R6
	MOVWU 20(R1), R7
	AND $1, R7, R7
	ORR R7<<5, R6, R6
	MOVWU 24(R1), R7
	AND $1, R7, R7
	ORR R7<<6, R6, R6
	MOVWU 28(R1), R7
	AND $1, R7, R7
	ORR R7<<7, R6, R6
	MOVB R6, (R0)
	ADD $32, R1, R1
	ADD $1, R0, R0
	ADD $8, R5, R5
	CMP R4, R5
	BLT neon_1bit_loop
	B neon_done

neon_2bit:
	MOVD R2, R4
	LSR $2, R4, R4
	LSL $2, R4, R4
	MOVD $0, R5
	CMP $0, R4
	BEQ neon_done

neon_2bit_loop:
	MOVWU (R1), R6
	AND $3, R6, R6
	MOVWU 4(R1), R7
	AND $3, R7, R7
	ORR R7<<2, R6, R6
	MOVWU 8(R1), R7
	AND $3, R7, R7
	ORR R7<<4, R6, R6
	MOVWU 12(R1), R7
	AND $3, R7, R7
	ORR R7<<6, R6, R6
	MOVB R6, (R0)
	ADD $16, R1, R1
	ADD $1, R0, R0
	ADD $4, R5, R5
	CMP R4, R5
	BLT neon_2bit_loop
	B neon_done

neon_3bit:
	MOVD R2, R4
	LSR $3, R4, R4
	LSL $3, R4, R4
	MOVD $0, R5
	CMP $0, R4
	BEQ neon_done

neon_3bit_loop:
	MOVWU (R1), R6
	AND $7, R6, R6
	MOVWU 4(R1), R7
	AND $7, R7, R7
	ORR R7<<3, R6, R6
	MOVWU 8(R1), R7
	AND $7, R7, R7
	ORR R7<<6, R6, R6
	MOVWU 12(R1), R7
	AND $7, R7, R7
	ORR R7<<9, R6, R6
	MOVWU 16(R1), R7
	AND $7, R7, R7
	ORR R7<<12, R6, R6
	MOVWU 20(R1), R7
	AND $7, R7, R7
	ORR R7<<15, R6, R6
	MOVWU 24(R1), R7
	AND $7, R7, R7
	ORR R7<<18, R6, R6
	MOVWU 28(R1), R7
	AND $7, R7, R7
	ORR R7<<21, R6, R6
	MOVB R6, (R0)
	LSR $8, R6, R7
	MOVB R7, 1(R0)
	LSR $16, R6, R7
	MOVB R7, 2(R0)
	ADD $32, R1, R1
	ADD $3, R0, R0
	ADD $8, R5, R5
	CMP R4, R5
	BLT neon_3bit_loop
	B neon_done

neon_4bit:
	MOVD R2, R4
	LSR $2, R4, R4
	LSL $2, R4, R4
	MOVD $0, R5
	CMP $0, R4
	BEQ neon_done

neon_4bit_loop:
	MOVWU (R1), R6
	AND $15, R6, R6
	MOVWU 4(R1), R7
	AND $15, R7, R7
	ORR R7<<4, R6, R6
	MOVWU 8(R1), R7
	AND $15, R7, R7
	ORR R7<<8, R6, R6
	MOVWU 12(R1), R7
	AND $15, R7, R7
	ORR R7<<12, R6, R6
	MOVH R6, (R0)
	ADD $16, R1, R1
	ADD $2, R0, R0
	ADD $4, R5, R5
	CMP R4, R5
	BLT neon_4bit_loop
	B neon_done

neon_5bit:
	MOVD R2, R4
	LSR $3, R4, R4
	LSL $3, R4, R4
	MOVD $0, R5
	CMP $0, R4
	BEQ neon_done

neon_5bit_loop:
	MOVD $0, R6
	MOVWU (R1), R7
	AND $31, R7, R7
	ORR R7, R6, R6
	MOVWU 4(R1), R7
	AND $31, R7, R7
	ORR R7<<5, R6, R6
	MOVWU 8(R1), R7
	AND $31, R7, R7
	ORR R7<<10, R6, R6
	MOVWU 12(R1), R7
	AND $31, R7, R7
	ORR R7<<15, R6, R6
	MOVWU 16(R1), R7
	AND $31, R7, R7
	ORR R7<<20, R6, R6
	MOVWU 20(R1), R7
	AND $31, R7, R7
	ORR R7<<25, R6, R6
	MOVWU 24(R1), R7
	AND $31, R7, R7
	ORR R7<<30, R6, R6
	MOVWU 28(R1), R7
	AND $31, R7, R7
	ORR R7<<35, R6, R6
	MOVB R6, (R0)
	LSR $8, R6, R7
	MOVB R7, 1(R0)
	LSR $16, R6, R7
	MOVB R7, 2(R0)
	LSR $24, R6, R7
	MOVB R7, 3(R0)
	LSR $32, R6, R7
	MOVB R7, 4(R0)
	ADD $32, R1, R1
	ADD $5, R0, R0
	ADD $8, R5, R5
	CMP R4, R5
	BLT neon_5bit_loop
	B neon_done

neon_6bit:
	MOVD R2, R4
	LSR $2, R4, R4
	LSL $2, R4, R4
	MOVD $0, R5
	CMP $0, R4
	BEQ neon_done

neon_6bit_loop:
	MOVWU (R1), R6
	AND $63, R6, R6
	MOVWU 4(R1), R7
	AND $63, R7, R7
	ORR R7<<6, R6, R6
	MOVWU 8(R1), R7
	AND $63, R7, R7
	ORR R7<<12, R6, R6
	MOVWU 12(R1), R7
	AND $63, R7, R7
	ORR R7<<18, R6, R6
	MOVB R6, (R0)
	LSR $8, R6, R7
	MOVB R7, 1(R0)
	LSR $16, R6, R7
	MOVB R7, 2(R0)
	ADD $16, R1, R1
	ADD $3, R0, R0
	ADD $4, R5, R5
	CMP R4, R5
	BLT neon_6bit_loop
	B neon_done

neon_7bit:
	MOVD R2, R4
	LSR $3, R4, R4
	LSL $3, R4, R4
	MOVD $0, R5
	CMP $0, R4
	BEQ neon_done

neon_7bit_loop:
	MOVD $0, R6
	MOVWU (R1), R7
	AND $127, R7, R7
	ORR R7, R6, R6
	MOVWU 4(R1), R7
	AND $127, R7, R7
	ORR R7<<7, R6, R6
	MOVWU 8(R1), R7
	AND $127, R7, R7
	ORR R7<<14, R6, R6
	MOVWU 12(R1), R7
	AND $127, R7, R7
	ORR R7<<21, R6, R6
	MOVWU 16(R1), R7
	AND $127, R7, R7
	ORR R7<<28, R6, R6
	MOVWU 20(R1), R7
	AND $127, R7, R7
	ORR R7<<35, R6, R6
	MOVWU 24(R1), R7
	AND $127, R7, R7
	ORR R7<<42, R6, R6
	MOVWU 28(R1), R7
	AND $127, R7, R7
	ORR R7<<49, R6, R6
	MOVB R6, (R0)
	LSR $8, R6, R7
	MOVB R7, 1(R0)
	LSR $16, R6, R7
	MOVB R7, 2(R0)
	LSR $24, R6, R7
	MOVB R7, 3(R0)
	LSR $32, R6, R7
	MOVB R7, 4(R0)
	LSR $40, R6, R7
	MOVB R7, 5(R0)
	LSR $48, R6, R7
	MOVB R7, 6(R0)
	ADD $32, R1, R1
	ADD $7, R0, R0
	ADD $8, R5, R5
	CMP R4, R5
	BLT neon_7bit_loop
	B neon_done

neon_8bit:
	MOVD R2, R4
	LSR $2, R4, R4
	LSL $2, R4, R4
	MOVD $0, R5
	CMP $0, R4
	BEQ neon_done

neon_8bit_loop:
	MOVWU (R1), R6
	MOVB R6, (R0)
	MOVWU 4(R1), R6
	MOVB R6, 1(R0)
	MOVWU 8(R1), R6
	MOVB R6, 2(R0)
	MOVWU 12(R1), R6
	MOVB R6, 3(R0)
	ADD $16, R1, R1
	ADD $4, R0, R0
	ADD $4, R5, R5
	CMP R4, R5
	BLT neon_8bit_loop

neon_done:
	// After NEON processing, handle any remainder with scalar code
	// Check if there are remaining values to process
	CMP R2, R5  // R5 = processed count, R2 = total length
	BGE neon_ret  // If processed >= total, we're done

	// Calculate remainder: adjust src/dst pointers and length
	// Advance src pointer by (R5 * 4) bytes
	LSL $2, R5, R16
	ADD R16, R1, R1

	// Calculate packed bytes for processed values and advance dst
	MUL R3, R5, R16  // R16 = processed * bitWidth (in bits)
	LSR $3, R16, R16  // R16 = packed bytes
	ADD R16, R0, R0

	// Update remaining length
	SUB R5, R2, R2

	// Jump to scalar implementation for remainder
	B ·packInt32ARM64(SB)

neon_ret:
	RET
