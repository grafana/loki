//go:build !purego

#include "funcdata.h"
#include "textflag.h"

// unpackInt64x2bitAVX2 implements optimized unpacking for bitWidth=2 using AVX2
// Each byte contains 4 x 2-bit values, processes 8 values at a time
//
// func unpackInt64x2bitAVX2(dst []int64, src []byte, bitWidth uint)
TEXT ·unpackInt64x2bitAVX2(SB), NOSPLIT, $0-56
	MOVQ dst_base+0(FP), AX   // AX = dst pointer
	MOVQ dst_len+8(FP), DX    // DX = dst length
	MOVQ src_base+24(FP), BX  // BX = src pointer
	MOVQ bitWidth+48(FP), CX  // CX = bitWidth (should be 2)

	// Check if we have at least 8 values to process
	CMPQ DX, $8
	JB avx2_2bit_tail

	// Round down to multiple of 8 for AVX2 processing
	MOVQ DX, DI
	SHRQ $3, DI      // DI = len / 8
	SHLQ $3, DI      // DI = aligned length (multiple of 8)
	XORQ SI, SI      // SI = index

avx2_2bit_loop:
	// Load 2 bytes (contains 8 x 2-bit values)
	MOVWQZX (BX), R8

	// Extract each 2-bit value and store as int64
	// Value 0 (bits 0-1)
	MOVQ R8, R9
	ANDQ $3, R9
	MOVQ R9, (AX)

	// Value 1 (bits 2-3)
	MOVQ R8, R9
	SHRQ $2, R9
	ANDQ $3, R9
	MOVQ R9, 8(AX)

	// Value 2 (bits 4-5)
	MOVQ R8, R9
	SHRQ $4, R9
	ANDQ $3, R9
	MOVQ R9, 16(AX)

	// Value 3 (bits 6-7)
	MOVQ R8, R9
	SHRQ $6, R9
	ANDQ $3, R9
	MOVQ R9, 24(AX)

	// Value 4 (bits 8-9)
	MOVQ R8, R9
	SHRQ $8, R9
	ANDQ $3, R9
	MOVQ R9, 32(AX)

	// Value 5 (bits 10-11)
	MOVQ R8, R9
	SHRQ $10, R9
	ANDQ $3, R9
	MOVQ R9, 40(AX)

	// Value 6 (bits 12-13)
	MOVQ R8, R9
	SHRQ $12, R9
	ANDQ $3, R9
	MOVQ R9, 48(AX)

	// Value 7 (bits 14-15)
	MOVQ R8, R9
	SHRQ $14, R9
	ANDQ $3, R9
	MOVQ R9, 56(AX)

	// Advance pointers
	ADDQ $2, BX      // src += 2 bytes
	ADDQ $64, AX     // dst += 8 int64 (64 bytes)
	ADDQ $8, SI      // index += 8

	CMPQ SI, DI
	JNE avx2_2bit_loop

avx2_2bit_tail:
	// Handle remaining elements with scalar fallback
	CMPQ SI, DX
	JE avx2_2bit_done

	// Compute remaining elements
	SUBQ SI, DX

	// Calculate bit offset for remaining elements
	// Each processed element consumes 2 bits, so bitOffset = SI * 2
	MOVQ SI, R9
	SHLQ $1, R9      // bitOffset = SI * 2
	XORQ R10, R10    // index = 0 (within remaining elements)
	JMP avx2_2bit_scalar_test

avx2_2bit_scalar_loop:
	MOVQ R9, R11
	SHRQ $3, R11     // byte_index = bitOffset / 8
	MOVQ src_base+24(FP), R14  // Get original src pointer
	MOVBQZX (R14)(R11*1), R12  // Load byte from original src

	MOVQ R9, R13
	ANDQ $7, R13     // bit_offset = bitOffset % 8

	MOVQ R13, CX     // Move bit offset to CX for shift
	SHRQ CL, R12     // Shift right by bit offset
	ANDQ $3, R12     // Mask to get 2 bits
	MOVQ R12, (AX)   // Store as int64

	ADDQ $8, AX      // dst++
	ADDQ $2, R9      // bitOffset += 2
	ADDQ $1, R10     // index++

avx2_2bit_scalar_test:
	CMPQ R10, DX
	JNE avx2_2bit_scalar_loop

avx2_2bit_done:
	RET
