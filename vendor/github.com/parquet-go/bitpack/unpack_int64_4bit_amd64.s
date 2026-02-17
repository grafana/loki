//go:build !purego

#include "funcdata.h"
#include "textflag.h"

// unpackInt64x4bitAVX2 implements optimized unpacking for bitWidth=4 using AVX2
// Each byte contains 2 x 4-bit values, processes 8 values at a time
//
// func unpackInt64x4bitAVX2(dst []int64, src []byte, bitWidth uint)
TEXT ·unpackInt64x4bitAVX2(SB), NOSPLIT, $0-56
	MOVQ dst_base+0(FP), AX   // AX = dst pointer
	MOVQ dst_len+8(FP), DX    // DX = dst length
	MOVQ src_base+24(FP), BX  // BX = src pointer
	MOVQ bitWidth+48(FP), CX  // CX = bitWidth (should be 4)

	// Check if we have at least 8 values to process
	CMPQ DX, $8
	JB avx2_4bit_tail

	// Round down to multiple of 8 for AVX2 processing
	MOVQ DX, DI
	SHRQ $3, DI      // DI = len / 8
	SHLQ $3, DI      // DI = aligned length (multiple of 8)
	XORQ SI, SI      // SI = index

avx2_4bit_loop:
	// Load 4 bytes (contains 8 x 4-bit values)
	MOVLQZX (BX), R8

	// Extract each 4-bit value and store as int64
	// Value 0 (bits 0-3)
	MOVQ R8, R9
	ANDQ $0xF, R9
	MOVQ R9, (AX)

	// Value 1 (bits 4-7)
	MOVQ R8, R9
	SHRQ $4, R9
	ANDQ $0xF, R9
	MOVQ R9, 8(AX)

	// Value 2 (bits 8-11)
	MOVQ R8, R9
	SHRQ $8, R9
	ANDQ $0xF, R9
	MOVQ R9, 16(AX)

	// Value 3 (bits 12-15)
	MOVQ R8, R9
	SHRQ $12, R9
	ANDQ $0xF, R9
	MOVQ R9, 24(AX)

	// Value 4 (bits 16-19)
	MOVQ R8, R9
	SHRQ $16, R9
	ANDQ $0xF, R9
	MOVQ R9, 32(AX)

	// Value 5 (bits 20-23)
	MOVQ R8, R9
	SHRQ $20, R9
	ANDQ $0xF, R9
	MOVQ R9, 40(AX)

	// Value 6 (bits 24-27)
	MOVQ R8, R9
	SHRQ $24, R9
	ANDQ $0xF, R9
	MOVQ R9, 48(AX)

	// Value 7 (bits 28-31)
	MOVQ R8, R9
	SHRQ $28, R9
	ANDQ $0xF, R9
	MOVQ R9, 56(AX)

	// Advance pointers
	ADDQ $4, BX      // src += 4 bytes
	ADDQ $64, AX     // dst += 8 int64 (64 bytes)
	ADDQ $8, SI      // index += 8

	CMPQ SI, DI
	JNE avx2_4bit_loop

avx2_4bit_tail:
	// Handle remaining elements with scalar fallback
	CMPQ SI, DX
	JE avx2_4bit_done

	// Compute remaining elements
	SUBQ SI, DX

	// Calculate bit offset for remaining elements
	// Each processed element consumes 4 bits, so bitOffset = SI * 4
	MOVQ SI, R9
	SHLQ $2, R9      // bitOffset = SI * 4
	XORQ R10, R10    // index = 0 (within remaining elements)
	JMP avx2_4bit_scalar_test

avx2_4bit_scalar_loop:
	MOVQ R9, R11
	SHRQ $3, R11     // byte_index = bitOffset / 8
	MOVQ src_base+24(FP), R14  // Get original src pointer
	MOVBQZX (R14)(R11*1), R12  // Load byte from original src

	MOVQ R9, R13
	ANDQ $7, R13     // bit_offset = bitOffset % 8

	MOVQ R13, CX     // Move bit offset to CX for shift
	SHRQ CL, R12     // Shift right by bit offset
	ANDQ $0xF, R12   // Mask to get 4 bits
	MOVQ R12, (AX)   // Store as int64

	ADDQ $8, AX      // dst++
	ADDQ $4, R9      // bitOffset += 4
	ADDQ $1, R10     // index++

avx2_4bit_scalar_test:
	CMPQ R10, DX
	JNE avx2_4bit_scalar_loop

avx2_4bit_done:
	RET
