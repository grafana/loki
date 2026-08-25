//go:build !purego

#include "funcdata.h"
#include "textflag.h"

// func packInt64ARM64(dst []byte, src []int64, bitWidth uint)
TEXT ·packInt64ARM64(SB), NOSPLIT, $0-56
	MOVD dst_base+0(FP), R0   // R0 = dst pointer
	MOVD src_base+24(FP), R1  // R1 = src pointer
	MOVD src_len+32(FP), R2   // R2 = src length
	MOVD bitWidth+48(FP), R3  // R3 = bitWidth

	// Handle bitWidth == 0
	CBZ R3, done

	// Special case: bitWidth == 64 (no packing needed)
	CMP $64, R3
	BEQ copy_direct

	// R4 = bitMask = (1 << bitWidth) - 1
	MOVD $1, R4
	LSL R3, R4, R4
	SUB $1, R4, R4

	// R5 = bufferLo (64-bit accumulator)
	// R6 = bufferHi (overflow buffer)
	// R7 = bufferedBits
	// R8 = byteIndex
	// R9 = loop counter (src index)
	MOVD $0, R5
	MOVD $0, R6
	MOVD $0, R7
	MOVD $0, R8
	MOVD $0, R9

	// Main loop: process each value from src
loop:
	CMP R2, R9
	BEQ flush_remaining

	// Load value from src[R9]
	LSL $3, R9, R16          // R16 = R9 * 8
	MOVD (R1)(R16), R10      // R10 = src[R9]

	// Mask the value: R10 = value & bitMask
	AND R4, R10, R10

	// Check if value fits entirely in low buffer
	ADD R3, R7, R11          // R11 = bufferedBits + bitWidth
	CMP $64, R11
	BGT spans_buffers

	// Value fits in low buffer
	LSL R7, R10, R12         // R12 = value << bufferedBits
	ORR R12, R5, R5          // bufferLo |= R12
	MOVD R11, R7             // bufferedBits = R11
	B increment_index

spans_buffers:
	// Value spans low and high buffers
	// bitsInLo = 64 - bufferedBits
	MOVD $64, R12
	SUB R7, R12, R12         // R12 = bitsInLo

	// bufferLo |= value << bufferedBits
	LSL R7, R10, R13
	ORR R13, R5, R5

	// bufferHi = value >> bitsInLo
	LSR R12, R10, R6

	// bufferedBits += bitWidth
	MOVD R11, R7

increment_index:
	// Increment source index
	ADD $1, R9, R9

flush_loop:
	// While bufferedBits >= 64, flush 64-bit words
	CMP $64, R7
	BLT loop

	// Write 64-bit word to dst[byteIndex]
	MOVD R5, (R0)(R8)

	// bufferLo = bufferHi
	MOVD R6, R5

	// bufferHi = 0
	MOVD $0, R6

	// bufferedBits -= 64
	SUB $64, R7, R7

	// byteIndex += 8
	ADD $8, R8, R8

	B flush_loop

flush_remaining:
	// If no bits remaining, we're done
	CBZ R7, done

	// Calculate remaining bytes = (bufferedBits + 7) / 8
	ADD $7, R7, R11
	LSR $3, R11, R11         // R11 = remainingBytes

	MOVD $0, R12             // R12 = i (byte counter)

flush_byte_loop:
	CMP R11, R12
	BEQ done

	// dst[byteIndex] = byte(bufferLo)
	MOVB R5, (R0)(R8)

	// bufferLo >>= 8
	LSR $8, R5, R5

	// byteIndex++, i++
	ADD $1, R8, R8
	ADD $1, R12, R12

	B flush_byte_loop

copy_direct:
	// bitWidth == 64: direct copy
	MOVD $0, R9              // R9 = index
	MOVD $0, R10             // R10 = byte offset

copy_loop:
	CMP R2, R9
	BEQ done

	// Load src[i]
	LSL $3, R9, R16
	MOVD (R1)(R16), R11

	// Store to dst[i*8]
	MOVD R11, (R0)(R10)

	// i++, offset += 8
	ADD $1, R9, R9
	ADD $8, R10, R10

	B copy_loop

done:
	RET

// func packInt64NEON(dst []byte, src []int64, bitWidth uint)
TEXT ·packInt64NEON(SB), NOSPLIT, $0-56
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
	// BitWidth 1: Pack 8 int64 values into 1 byte
	MOVD R2, R4
	LSR $3, R4, R4      // R4 = len / 8
	LSL $3, R4, R4      // R4 = aligned length
	MOVD $0, R5         // R5 = index
	CMP $0, R4
	BEQ neon_done

neon_1bit_loop:
	MOVD (R1), R6
	AND $1, R6, R6
	MOVD 8(R1), R7
	AND $1, R7, R7
	ORR R7<<1, R6, R6
	MOVD 16(R1), R7
	AND $1, R7, R7
	ORR R7<<2, R6, R6
	MOVD 24(R1), R7
	AND $1, R7, R7
	ORR R7<<3, R6, R6
	MOVD 32(R1), R7
	AND $1, R7, R7
	ORR R7<<4, R6, R6
	MOVD 40(R1), R7
	AND $1, R7, R7
	ORR R7<<5, R6, R6
	MOVD 48(R1), R7
	AND $1, R7, R7
	ORR R7<<6, R6, R6
	MOVD 56(R1), R7
	AND $1, R7, R7
	ORR R7<<7, R6, R6
	MOVB R6, (R0)
	ADD $64, R1, R1
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
	MOVD (R1), R6
	AND $3, R6, R6
	MOVD 8(R1), R7
	AND $3, R7, R7
	ORR R7<<2, R6, R6
	MOVD 16(R1), R7
	AND $3, R7, R7
	ORR R7<<4, R6, R6
	MOVD 24(R1), R7
	AND $3, R7, R7
	ORR R7<<6, R6, R6
	MOVB R6, (R0)
	ADD $32, R1, R1
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
	MOVD (R1), R6
	AND $7, R6, R6
	MOVD 8(R1), R7
	AND $7, R7, R7
	ORR R7<<3, R6, R6
	MOVD 16(R1), R7
	AND $7, R7, R7
	ORR R7<<6, R6, R6
	MOVD 24(R1), R7
	AND $7, R7, R7
	ORR R7<<9, R6, R6
	MOVD 32(R1), R7
	AND $7, R7, R7
	ORR R7<<12, R6, R6
	MOVD 40(R1), R7
	AND $7, R7, R7
	ORR R7<<15, R6, R6
	MOVD 48(R1), R7
	AND $7, R7, R7
	ORR R7<<18, R6, R6
	MOVD 56(R1), R7
	AND $7, R7, R7
	ORR R7<<21, R6, R6
	MOVB R6, (R0)
	LSR $8, R6, R7
	MOVB R7, 1(R0)
	LSR $16, R6, R7
	MOVB R7, 2(R0)
	ADD $64, R1, R1
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
	MOVD (R1), R6
	AND $15, R6, R6
	MOVD 8(R1), R7
	AND $15, R7, R7
	ORR R7<<4, R6, R6
	MOVD 16(R1), R7
	AND $15, R7, R7
	ORR R7<<8, R6, R6
	MOVD 24(R1), R7
	AND $15, R7, R7
	ORR R7<<12, R6, R6
	MOVH R6, (R0)
	ADD $32, R1, R1
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
	MOVD (R1), R7
	AND $31, R7, R7
	ORR R7, R6, R6
	MOVD 8(R1), R7
	AND $31, R7, R7
	ORR R7<<5, R6, R6
	MOVD 16(R1), R7
	AND $31, R7, R7
	ORR R7<<10, R6, R6
	MOVD 24(R1), R7
	AND $31, R7, R7
	ORR R7<<15, R6, R6
	MOVD 32(R1), R7
	AND $31, R7, R7
	ORR R7<<20, R6, R6
	MOVD 40(R1), R7
	AND $31, R7, R7
	ORR R7<<25, R6, R6
	MOVD 48(R1), R7
	AND $31, R7, R7
	ORR R7<<30, R6, R6
	MOVD 56(R1), R7
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
	ADD $64, R1, R1
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
	MOVD (R1), R6
	AND $63, R6, R6
	MOVD 8(R1), R7
	AND $63, R7, R7
	ORR R7<<6, R6, R6
	MOVD 16(R1), R7
	AND $63, R7, R7
	ORR R7<<12, R6, R6
	MOVD 24(R1), R7
	AND $63, R7, R7
	ORR R7<<18, R6, R6
	MOVB R6, (R0)
	LSR $8, R6, R7
	MOVB R7, 1(R0)
	LSR $16, R6, R7
	MOVB R7, 2(R0)
	ADD $32, R1, R1
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
	MOVD (R1), R7
	AND $127, R7, R7
	ORR R7, R6, R6
	MOVD 8(R1), R7
	AND $127, R7, R7
	ORR R7<<7, R6, R6
	MOVD 16(R1), R7
	AND $127, R7, R7
	ORR R7<<14, R6, R6
	MOVD 24(R1), R7
	AND $127, R7, R7
	ORR R7<<21, R6, R6
	MOVD 32(R1), R7
	AND $127, R7, R7
	ORR R7<<28, R6, R6
	MOVD 40(R1), R7
	AND $127, R7, R7
	ORR R7<<35, R6, R6
	MOVD 48(R1), R7
	AND $127, R7, R7
	ORR R7<<42, R6, R6
	MOVD 56(R1), R7
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
	ADD $64, R1, R1
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
	MOVD (R1), R6
	MOVB R6, (R0)
	MOVD 8(R1), R6
	MOVB R6, 1(R0)
	MOVD 16(R1), R6
	MOVB R6, 2(R0)
	MOVD 24(R1), R6
	MOVB R6, 3(R0)
	ADD $32, R1, R1
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
	// Advance src pointer by (R5 * 8) bytes
	LSL $3, R5, R16
	ADD R16, R1, R1

	// Calculate packed bytes for processed values and advance dst
	MUL R3, R5, R16  // R16 = processed * bitWidth (in bits)
	LSR $3, R16, R16  // R16 = packed bytes
	ADD R16, R0, R0

	// Update remaining length
	SUB R5, R2, R2

	// Jump to scalar implementation for remainder
	B ·packInt64ARM64(SB)

neon_ret:
	RET
