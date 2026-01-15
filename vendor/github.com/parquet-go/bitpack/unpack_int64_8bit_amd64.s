//go:build !purego

#include "funcdata.h"
#include "textflag.h"

// unpackInt64x8bitAVX2 implements optimized unpacking for bitWidth=8 using AVX2
// Each byte is already a complete value, processes 8 values at a time
//
// func unpackInt64x8bitAVX2(dst []int64, src []byte, bitWidth uint)
TEXT ·unpackInt64x8bitAVX2(SB), NOSPLIT, $0-56
	MOVQ dst_base+0(FP), AX   // AX = dst pointer
	MOVQ dst_len+8(FP), DX    // DX = dst length
	MOVQ src_base+24(FP), BX  // BX = src pointer
	MOVQ bitWidth+48(FP), CX  // CX = bitWidth (should be 8)

	// Check if we have at least 8 values to process
	CMPQ DX, $8
	JB avx2_8bit_tail

	// Round down to multiple of 8 for AVX2 processing
	MOVQ DX, DI
	SHRQ $3, DI      // DI = len / 8
	SHLQ $3, DI      // DI = aligned length (multiple of 8)
	XORQ SI, SI      // SI = index

avx2_8bit_loop:
	// Load 8 bytes (8 x 8-bit values)
	MOVQ (BX), R8

	// Extract each byte and store as int64
	// Value 0 (byte 0)
	MOVQ R8, R9
	ANDQ $0xFF, R9
	MOVQ R9, (AX)

	// Value 1 (byte 1)
	MOVQ R8, R9
	SHRQ $8, R9
	ANDQ $0xFF, R9
	MOVQ R9, 8(AX)

	// Value 2 (byte 2)
	MOVQ R8, R9
	SHRQ $16, R9
	ANDQ $0xFF, R9
	MOVQ R9, 16(AX)

	// Value 3 (byte 3)
	MOVQ R8, R9
	SHRQ $24, R9
	ANDQ $0xFF, R9
	MOVQ R9, 24(AX)

	// Value 4 (byte 4)
	MOVQ R8, R9
	SHRQ $32, R9
	ANDQ $0xFF, R9
	MOVQ R9, 32(AX)

	// Value 5 (byte 5)
	MOVQ R8, R9
	SHRQ $40, R9
	ANDQ $0xFF, R9
	MOVQ R9, 40(AX)

	// Value 6 (byte 6)
	MOVQ R8, R9
	SHRQ $48, R9
	ANDQ $0xFF, R9
	MOVQ R9, 48(AX)

	// Value 7 (byte 7)
	MOVQ R8, R9
	SHRQ $56, R9
	MOVQ R9, 56(AX)

	// Advance pointers
	ADDQ $8, BX      // src += 8 bytes
	ADDQ $64, AX     // dst += 8 int64 (64 bytes)
	ADDQ $8, SI      // index += 8

	CMPQ SI, DI
	JNE avx2_8bit_loop

avx2_8bit_tail:
	// Handle remaining elements with scalar fallback
	CMPQ SI, DX
	JE avx2_8bit_done

	// Compute remaining elements
	SUBQ SI, DX

avx2_8bit_tail_loop:
	MOVBQZX (BX), R8  // Load byte
	MOVQ R8, (AX)     // Store as int64 (zero-extended)

	ADDQ $1, BX       // src++
	ADDQ $8, AX       // dst++
	DECQ DX           // remaining--

	CMPQ DX, $0
	JNE avx2_8bit_tail_loop

avx2_8bit_done:
	RET
