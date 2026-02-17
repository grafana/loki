//go:build !purego

#include "funcdata.h"
#include "textflag.h"

// unpackInt64x1bitAVX2 implements optimized unpacking for bitWidth=1 using AVX2
// Each byte contains 8 bits, processes 8 values at a time
//
// func unpackInt64x1bitAVX2(dst []int64, src []byte, bitWidth uint)
TEXT ·unpackInt64x1bitAVX2(SB), NOSPLIT, $0-56
	MOVQ dst_base+0(FP), AX   // AX = dst pointer
	MOVQ dst_len+8(FP), DX    // DX = dst length
	MOVQ src_base+24(FP), BX  // BX = src pointer
	MOVQ bitWidth+48(FP), CX  // CX = bitWidth (should be 1)

	// Check if we have at least 8 values to process
	CMPQ DX, $8
	JB avx2_1bit_tail

	// Round down to multiple of 8 for AVX2 processing
	MOVQ DX, DI
	SHRQ $3, DI      // DI = len / 8
	SHLQ $3, DI      // DI = aligned length (multiple of 8)
	XORQ SI, SI      // SI = index

avx2_1bit_loop:
	// Load 1 byte (contains 8 x 1-bit values)
	MOVBQZX (BX), R8

	// Extract each bit and store as int64
	// Bit 0
	MOVQ R8, R9
	ANDQ $1, R9
	MOVQ R9, (AX)

	// Bit 1
	MOVQ R8, R9
	SHRQ $1, R9
	ANDQ $1, R9
	MOVQ R9, 8(AX)

	// Bit 2
	MOVQ R8, R9
	SHRQ $2, R9
	ANDQ $1, R9
	MOVQ R9, 16(AX)

	// Bit 3
	MOVQ R8, R9
	SHRQ $3, R9
	ANDQ $1, R9
	MOVQ R9, 24(AX)

	// Bit 4
	MOVQ R8, R9
	SHRQ $4, R9
	ANDQ $1, R9
	MOVQ R9, 32(AX)

	// Bit 5
	MOVQ R8, R9
	SHRQ $5, R9
	ANDQ $1, R9
	MOVQ R9, 40(AX)

	// Bit 6
	MOVQ R8, R9
	SHRQ $6, R9
	ANDQ $1, R9
	MOVQ R9, 48(AX)

	// Bit 7
	MOVQ R8, R9
	SHRQ $7, R9
	ANDQ $1, R9
	MOVQ R9, 56(AX)

	// Advance pointers
	ADDQ $1, BX      // src += 1 byte
	ADDQ $64, AX     // dst += 8 int64 (64 bytes)
	ADDQ $8, SI      // index += 8

	CMPQ SI, DI
	JNE avx2_1bit_loop

avx2_1bit_tail:
	// Handle remaining elements with scalar fallback
	CMPQ SI, DX
	JE avx2_1bit_done

	// Compute remaining elements
	SUBQ SI, DX

	// Calculate bit offset for remaining elements
	// Each processed element consumes 1 bit, so bitOffset = SI * 1
	MOVQ SI, R9      // bitOffset = SI (number of bits already processed)
	XORQ R10, R10    // index = 0 (within remaining elements)
	JMP avx2_1bit_scalar_test

avx2_1bit_scalar_loop:
	MOVQ R9, R11
	SHRQ $3, R11     // byte_index = bitOffset / 8
	MOVQ src_base+24(FP), R14  // Get original src pointer
	MOVBQZX (R14)(R11*1), R12  // Load byte from original src

	MOVQ R9, R13
	ANDQ $7, R13     // bit_offset = bitOffset % 8

	MOVQ R13, CX     // Move bit offset to CX for shift
	SHRQ CL, R12     // Shift right by bit offset
	ANDQ $1, R12     // Mask to get bit
	MOVQ R12, (AX)   // Store as int64

	ADDQ $8, AX      // dst++
	ADDQ $1, R9      // bitOffset++
	ADDQ $1, R10     // index++

avx2_1bit_scalar_test:
	CMPQ R10, DX
	JNE avx2_1bit_scalar_loop

avx2_1bit_done:
	RET
