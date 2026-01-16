//go:build !purego

#include "funcdata.h"
#include "textflag.h"

// func unpackInt64Default(dst []int64, src []uint32, bitWidth uint)
TEXT ·unpackInt64Default(SB), NOSPLIT, $0-56
    MOVQ dst_base+0(FP), AX
    MOVQ dst_len+8(FP), DX
    MOVQ src_base+24(FP), BX
    MOVQ bitWidth+48(FP), CX

    // Initialize
    XORQ DI, DI // bitOffset
    XORQ SI, SI // index

    // Check if length >= 4 for unrolled loop
    CMPQ DX, $4
    JB scalar_loop_start

    // Calculate bitMask = (1 << bitWidth) - 1
    MOVQ $1, R8
    SHLQ CX, R8
    DECQ R8

    // Calculate unrolled iterations: (length / 4) * 4
    MOVQ DX, R15
    SHRQ $2, R15          // R15 = length / 4
    JZ scalar_loop_start
    SHLQ $2, R15          // R15 = (length / 4) * 4

unrolled_loop:
    // Process 4 elements with 64-bit loads

    // === Element 0 ===
    MOVQ DI, R10
    SHRQ $6, R10          // i = bitOffset / 64
    MOVQ DI, R9
    ANDQ $63, R9          // j = bitOffset % 64
    MOVQ (BX)(R10*8), R11 // load 64-bit word
    MOVQ R8, R12
    MOVQ R9, CX
    SHLQ CL, R12
    ANDQ R12, R11
    SHRQ CL, R11

    // Check if spans to next word
    MOVQ R9, R13
    ADDQ bitWidth+48(FP), R13
    CMPQ R13, $64
    JBE store0
    MOVQ $64, CX
    SUBQ R9, CX
    MOVQ 8(BX)(R10*8), R14
    MOVQ R8, R12
    SHRQ CL, R12
    ANDQ R12, R14
    SHLQ CL, R14
    ORQ R14, R11

store0:
    ADDQ bitWidth+48(FP), DI
    MOVQ R11, (AX)(SI*8)
    INCQ SI

    // === Element 1 ===
    MOVQ DI, R10
    SHRQ $6, R10
    MOVQ DI, R9
    ANDQ $63, R9
    MOVQ (BX)(R10*8), R11
    MOVQ R8, R12
    MOVQ R9, CX
    SHLQ CL, R12
    ANDQ R12, R11
    SHRQ CL, R11

    MOVQ R9, R13
    ADDQ bitWidth+48(FP), R13
    CMPQ R13, $64
    JBE store1
    MOVQ $64, CX
    SUBQ R9, CX
    MOVQ 8(BX)(R10*8), R14
    MOVQ R8, R12
    SHRQ CL, R12
    ANDQ R12, R14
    SHLQ CL, R14
    ORQ R14, R11

store1:
    ADDQ bitWidth+48(FP), DI
    MOVQ R11, (AX)(SI*8)
    INCQ SI

    // === Element 2 ===
    MOVQ DI, R10
    SHRQ $6, R10
    MOVQ DI, R9
    ANDQ $63, R9
    MOVQ (BX)(R10*8), R11
    MOVQ R8, R12
    MOVQ R9, CX
    SHLQ CL, R12
    ANDQ R12, R11
    SHRQ CL, R11

    MOVQ R9, R13
    ADDQ bitWidth+48(FP), R13
    CMPQ R13, $64
    JBE store2
    MOVQ $64, CX
    SUBQ R9, CX
    MOVQ 8(BX)(R10*8), R14
    MOVQ R8, R12
    SHRQ CL, R12
    ANDQ R12, R14
    SHLQ CL, R14
    ORQ R14, R11

store2:
    ADDQ bitWidth+48(FP), DI
    MOVQ R11, (AX)(SI*8)
    INCQ SI

    // === Element 3 ===
    MOVQ DI, R10
    SHRQ $6, R10
    MOVQ DI, R9
    ANDQ $63, R9
    MOVQ (BX)(R10*8), R11
    MOVQ R8, R12
    MOVQ R9, CX
    SHLQ CL, R12
    ANDQ R12, R11
    SHRQ CL, R11

    MOVQ R9, R13
    ADDQ bitWidth+48(FP), R13
    CMPQ R13, $64
    JBE store3
    MOVQ $64, CX
    SUBQ R9, CX
    MOVQ 8(BX)(R10*8), R14
    MOVQ R8, R12
    SHRQ CL, R12
    ANDQ R12, R14
    SHLQ CL, R14
    ORQ R14, R11

store3:
    ADDQ bitWidth+48(FP), DI
    MOVQ R11, (AX)(SI*8)
    INCQ SI

    CMPQ SI, R15
    JB unrolled_loop

    // Check if done
    CMPQ SI, DX
    JE done

scalar_loop_start:
    // Fallback scalar loop for remaining elements
    // Check if there are any elements to process
    CMPQ SI, DX
    JE done
    
    MOVQ bitWidth+48(FP), CX
    MOVQ $1, R8
    SHLQ CX, R8
    DECQ R8

scalar_loop:
    // i = bitOffset / 64
    MOVQ DI, R10
    SHRQ $6, R10
    
    // j = bitOffset % 64
    MOVQ DI, R9
    ANDQ $63, R9
    
    // Load 64-bit word and extract
    MOVQ (BX)(R10*8), R11
    MOVQ R8, R12
    MOVQ R9, CX
    SHLQ CL, R12
    ANDQ R12, R11
    SHRQ CL, R11
    
    // Check for span
    MOVQ R9, R13
    ADDQ bitWidth+48(FP), R13
    CMPQ R13, $64
    JBE scalar_next
    
    MOVQ $64, CX
    SUBQ R9, CX
    MOVQ 8(BX)(R10*8), R14
    MOVQ R8, R12
    SHRQ CL, R12
    ANDQ R12, R14
    SHLQ CL, R14
    ORQ R14, R11
    
scalar_next:
    MOVQ R11, (AX)(SI*8)
    ADDQ bitWidth+48(FP), DI
    INCQ SI
    CMPQ SI, DX
    JNE scalar_loop
    JMP done

zero_fill:
    // Fill output with zeros for bitWidth==0
    XORQ SI, SI      // Initialize index
    XORQ R8, R8      // Zero value
zero_loop:
    CMPQ SI, DX
    JE done
    MOVQ R8, (AX)(SI*8)
    INCQ SI
    JMP zero_loop

done:
    RET

// This bit unpacking function was inspired from the 32 bit version, but
// adapted to account for the fact that eight 64 bit values span across
// two YMM registers, and across lanes of YMM registers.
//
// Because of the two lanes of YMM registers, we cannot use the VPSHUFB
// instruction to dispatch bytes of the input to the registers. Instead we use
// the VPERMD instruction, which has higher latency but supports dispatching
// bytes across register lanes. Measurable throughput gains remain despite the
// algorithm running on a few more CPU cycles per loop.
//
// The initialization phase of this algorithm generates masks for
// permutations and shifts used to decode the bit-packed values.
//
// The permutation masks are written to Y7 and Y8, and contain the results
// of this formula:
//
//      temp[i] = (bitWidth * i) / 32
//      mask[i] = temp[i] | ((temp[i] + 1) << 32)
//
// Since VPERMQ only supports reading the permutation combination from an
// immediate value, we use VPERMD and generate permutation for pairs of two
// consecutive 32 bit words, which is why we have the upper part of each 64
// bit word set with (x+1)<<32.
//
// The masks for right shifts are written to Y5 and Y6, and computed with
// this formula:
//
//      shift[i] = (bitWidth * i) - (32 * ((bitWidth * i) / 32))
//
// The amount to shift by is the number of values previously unpacked, offseted
// by the byte count of 32 bit words that we read from first bits from.
//
// Technically the masks could be precomputed and declared in global tables;
// however, declaring masks for all bit width is tedious and makes code
// maintenance more costly for no measurable benefits on production workloads.
//
// unpackInt64x1to32bitsAVX2 implements optimized unpacking for bit widths 1-32
// Uses specialized kernels for common bit widths with batched processing
//
// func unpackInt64x1to32bitsAVX2(dst []int64, src []byte, bitWidth uint)
TEXT ·unpackInt64x1to32bitsAVX2(SB), NOSPLIT, $56-56
    NO_LOCAL_POINTERS
    MOVQ dst_base+0(FP), AX
    MOVQ dst_len+8(FP), DX
    MOVQ src_base+24(FP), BX
    MOVQ bitWidth+48(FP), CX

    // Check if we have enough values for specialized kernels
    // 1-bit needs 8, 2-bit needs 8, 4-bit needs 8, 8-bit needs 8
    // 16-bit needs 4, 32-bit needs 2
    CMPQ CX, $1
    JE check_1bit
    CMPQ CX, $2
    JE check_2bit
    CMPQ CX, $3
    JE check_3bit
    CMPQ CX, $4
    JE check_4bit
    CMPQ CX, $5
    JE check_5bit
    CMPQ CX, $6
    JE check_6bit
    CMPQ CX, $7
    JE check_7bit
    CMPQ CX, $8
    JE check_8bit
    CMPQ CX, $16
    JE check_16bit
    CMPQ CX, $32
    JE check_32bit

    // For other bit widths, check if we have at least 8 values
    CMPQ DX, $8
    JB tail
    JMP generic_avx2

check_1bit:
    CMPQ DX, $8
    JB tail
    JMP int64_1bit

check_2bit:
    CMPQ DX, $8
    JB tail
    JMP int64_2bit

check_3bit:
    CMPQ DX, $8
    JB tail
    JMP int64_3bit

check_4bit:
    CMPQ DX, $8
    JB tail
    JMP int64_4bit

check_5bit:
    CMPQ DX, $8
    JB tail
    JMP int64_5bit

check_6bit:
    CMPQ DX, $8
    JB tail
    JMP int64_6bit

check_7bit:
    CMPQ DX, $8
    JB tail
    JMP int64_7bit

check_8bit:
    CMPQ DX, $8
    JB tail
    JMP int64_8bit

check_16bit:
    CMPQ DX, $4
    JB tail
    JMP int64_16bit

check_32bit:
    CMPQ DX, $2
    JB tail
    JMP int64_32bit


int64_1bit:
    // Call specialized 1-bit kernel
    MOVQ AX, dst_base-56(SP)
    MOVQ DX, dst_len-48(SP)
    MOVQ BX, src_base-32(SP)
    MOVQ CX, bitWidth-8(SP)
    CALL ·unpackInt64x1bitAVX2(SB)
    RET

int64_2bit:
    // Call specialized 2-bit kernel
    MOVQ AX, dst_base-56(SP)
    MOVQ DX, dst_len-48(SP)
    MOVQ BX, src_base-32(SP)
    MOVQ CX, bitWidth-8(SP)
    CALL ·unpackInt64x2bitAVX2(SB)
    RET

int64_3bit:
    // BitWidth 3: 8 int64 values packed in 3 bytes
    CMPQ DX, $8
    JB tail

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI
    XORQ SI, SI

int64_3bit_loop:
    MOVLQZX (BX), R8

    MOVQ R8, R9
    ANDQ $7, R9
    MOVQ R9, (AX)

    MOVQ R8, R9
    SHRQ $3, R9
    ANDQ $7, R9
    MOVQ R9, 8(AX)

    MOVQ R8, R9
    SHRQ $6, R9
    ANDQ $7, R9
    MOVQ R9, 16(AX)

    MOVQ R8, R9
    SHRQ $9, R9
    ANDQ $7, R9
    MOVQ R9, 24(AX)

    MOVQ R8, R9
    SHRQ $12, R9
    ANDQ $7, R9
    MOVQ R9, 32(AX)

    MOVQ R8, R9
    SHRQ $15, R9
    ANDQ $7, R9
    MOVQ R9, 40(AX)

    MOVQ R8, R9
    SHRQ $18, R9
    ANDQ $7, R9
    MOVQ R9, 48(AX)

    MOVQ R8, R9
    SHRQ $21, R9
    ANDQ $7, R9
    MOVQ R9, 56(AX)

    ADDQ $3, BX
    ADDQ $64, AX
    ADDQ $8, SI

    CMPQ SI, DI
    JNE int64_3bit_loop

    CMPQ SI, DX
    JE done
    SUBQ SI, DX
    JMP tail

int64_4bit:
    // Call specialized 4-bit kernel
    MOVQ AX, dst_base-56(SP)
    MOVQ DX, dst_len-48(SP)
    MOVQ BX, src_base-32(SP)
    MOVQ CX, bitWidth-8(SP)
    CALL ·unpackInt64x4bitAVX2(SB)
    RET

int64_5bit:
    // BitWidth 5: 8 int64 values packed in 5 bytes
    CMPQ DX, $8
    JB tail

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI
    XORQ SI, SI

int64_5bit_loop:
    MOVQ (BX), R8

    MOVQ R8, R9
    ANDQ $31, R9
    MOVQ R9, (AX)

    MOVQ R8, R9
    SHRQ $5, R9
    ANDQ $31, R9
    MOVQ R9, 8(AX)

    MOVQ R8, R9
    SHRQ $10, R9
    ANDQ $31, R9
    MOVQ R9, 16(AX)

    MOVQ R8, R9
    SHRQ $15, R9
    ANDQ $31, R9
    MOVQ R9, 24(AX)

    MOVQ R8, R9
    SHRQ $20, R9
    ANDQ $31, R9
    MOVQ R9, 32(AX)

    MOVQ R8, R9
    SHRQ $25, R9
    ANDQ $31, R9
    MOVQ R9, 40(AX)

    MOVQ R8, R9
    SHRQ $30, R9
    ANDQ $31, R9
    MOVQ R9, 48(AX)

    MOVQ R8, R9
    SHRQ $35, R9
    ANDQ $31, R9
    MOVQ R9, 56(AX)

    ADDQ $5, BX
    ADDQ $64, AX
    ADDQ $8, SI

    CMPQ SI, DI
    JNE int64_5bit_loop

    CMPQ SI, DX
    JE done
    SUBQ SI, DX
    JMP tail

int64_6bit:
    // BitWidth 6: 8 int64 values packed in 6 bytes
    CMPQ DX, $8
    JB tail

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI
    XORQ SI, SI

int64_6bit_loop:
    MOVQ (BX), R8

    MOVQ R8, R9
    ANDQ $63, R9
    MOVQ R9, (AX)

    MOVQ R8, R9
    SHRQ $6, R9
    ANDQ $63, R9
    MOVQ R9, 8(AX)

    MOVQ R8, R9
    SHRQ $12, R9
    ANDQ $63, R9
    MOVQ R9, 16(AX)

    MOVQ R8, R9
    SHRQ $18, R9
    ANDQ $63, R9
    MOVQ R9, 24(AX)

    MOVQ R8, R9
    SHRQ $24, R9
    ANDQ $63, R9
    MOVQ R9, 32(AX)

    MOVQ R8, R9
    SHRQ $30, R9
    ANDQ $63, R9
    MOVQ R9, 40(AX)

    MOVQ R8, R9
    SHRQ $36, R9
    ANDQ $63, R9
    MOVQ R9, 48(AX)

    MOVQ R8, R9
    SHRQ $42, R9
    ANDQ $63, R9
    MOVQ R9, 56(AX)

    ADDQ $6, BX
    ADDQ $64, AX
    ADDQ $8, SI

    CMPQ SI, DI
    JNE int64_6bit_loop

    CMPQ SI, DX
    JE done
    SUBQ SI, DX
    JMP tail

int64_7bit:
    // BitWidth 7: 8 int64 values packed in 7 bytes
    CMPQ DX, $8
    JB tail

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI
    XORQ SI, SI

int64_7bit_loop:
    MOVQ (BX), R8

    MOVQ R8, R9
    ANDQ $127, R9
    MOVQ R9, (AX)

    MOVQ R8, R9
    SHRQ $7, R9
    ANDQ $127, R9
    MOVQ R9, 8(AX)

    MOVQ R8, R9
    SHRQ $14, R9
    ANDQ $127, R9
    MOVQ R9, 16(AX)

    MOVQ R8, R9
    SHRQ $21, R9
    ANDQ $127, R9
    MOVQ R9, 24(AX)

    MOVQ R8, R9
    SHRQ $28, R9
    ANDQ $127, R9
    MOVQ R9, 32(AX)

    MOVQ R8, R9
    SHRQ $35, R9
    ANDQ $127, R9
    MOVQ R9, 40(AX)

    MOVQ R8, R9
    SHRQ $42, R9
    ANDQ $127, R9
    MOVQ R9, 48(AX)

    MOVQ R8, R9
    SHRQ $49, R9
    ANDQ $127, R9
    MOVQ R9, 56(AX)

    ADDQ $7, BX
    ADDQ $64, AX
    ADDQ $8, SI

    CMPQ SI, DI
    JNE int64_7bit_loop

    CMPQ SI, DX
    JE done
    SUBQ SI, DX
    JMP tail

int64_8bit:
    // Call specialized 8-bit kernel
    MOVQ AX, dst_base-56(SP)
    MOVQ DX, dst_len-48(SP)
    MOVQ BX, src_base-32(SP)
    MOVQ CX, bitWidth-8(SP)
    CALL ·unpackInt64x8bitAVX2(SB)
    RET

int64_16bit:
    // BitWidth 16: 4 int64 values packed in 8 bytes
    // Process 4 values at a time

    MOVQ DX, DI
    SHRQ $2, DI      // DI = len / 4
    SHLQ $2, DI      // DI = aligned length (multiple of 4)
    XORQ SI, SI      // SI = index

    CMPQ DI, $0
    JE tail

int64_16bit_loop:
    // Load 8 bytes as 4 uint16 values
    MOVQ (BX), R8

    // Extract 16-bit values and write as int64
    // Value 0 (bits 0-15)
    MOVQ R8, R9
    ANDQ $0xFFFF, R9
    MOVQ R9, (AX)

    // Value 1 (bits 16-31)
    MOVQ R8, R9
    SHRQ $16, R9
    ANDQ $0xFFFF, R9
    MOVQ R9, 8(AX)

    // Value 2 (bits 32-47)
    MOVQ R8, R9
    SHRQ $32, R9
    ANDQ $0xFFFF, R9
    MOVQ R9, 16(AX)

    // Value 3 (bits 48-63)
    MOVQ R8, R9
    SHRQ $48, R9
    MOVQ R9, 24(AX)

    // Advance pointers
    ADDQ $8, BX      // src += 8 bytes (4 values)
    ADDQ $32, AX     // dst += 4 int64 (32 bytes)
    ADDQ $4, SI      // index += 4

    CMPQ SI, DI
    JNE int64_16bit_loop

    // Handle tail with scalar
    CMPQ SI, DX
    JE done

    LEAQ (AX), AX    // AX already points to correct position
    SUBQ SI, DX
    JMP tail

int64_32bit:
    // BitWidth 32: Each value is exactly 4 bytes
    // Process values one at a time for simplicity

    MOVQ DX, DI      // DI = total length
    XORQ SI, SI      // SI = index

    CMPQ DI, $0
    JE done

int64_32bit_loop:
    // Load 4 bytes as one uint32 value
    MOVLQZX (BX), R8  // Load 32-bit value, zero-extend to 64-bit
    MOVQ R8, (AX)     // Store as int64

    // Advance pointers
    ADDQ $4, BX       // src += 4 bytes (1 value)
    ADDQ $8, AX       // dst += 1 int64 (8 bytes)
    ADDQ $1, SI       // index += 1

    CMPQ SI, DI
    JNE int64_32bit_loop

    // All values processed
    JMP done

generic_avx2:
    // Optimized AVX2 with reduced setup overhead
    CMPQ DX, $8
    JB tail

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI
    XORQ SI, SI

    // Compute bitMask
    MOVQ $1, R8
    SHLQ CX, R8
    DECQ R8
    MOVQ R8, X0
    VPBROADCASTQ X0, Y0

    // Use pre-computed table for bitWidths 9-31
    CMPQ CX, $9
    JB compute_masks
    CMPQ CX, $31
    JA compute_masks
    
    // Calculate table offset: (bitWidth - 9) * 128
    MOVQ CX, R9
    SUBQ $9, R9
    SHLQ $7, R9  // multiply by 128
    
    // Load pre-computed permutation and shift masks from table
    LEAQ ·permuteInt64Table(SB), R10
    VMOVDQU (R10)(R9*1), Y7
    VMOVDQU 32(R10)(R9*1), Y8
    VMOVDQU 64(R10)(R9*1), Y5
    VMOVDQU 96(R10)(R9*1), Y6
    JMP generic_loop

compute_masks:
    // Fallback: compute masks dynamically (original code)
    VPCMPEQQ Y1, Y1, Y1
    VPSRLQ $63, Y1, Y1
    MOVQ CX, X2
    VPBROADCASTQ X2, Y2
    VMOVDQU range0n7<>+0(SB), Y3
    VMOVDQU range0n7<>+32(SB), Y4
    VPMULLD Y2, Y3, Y5
    VPMULLD Y2, Y4, Y6
    VPSRLQ $5, Y5, Y7
    VPSRLQ $5, Y6, Y8
    VPSLLQ $5, Y7, Y9
    VPSLLQ $5, Y8, Y10
    VPADDQ Y1, Y7, Y11
    VPADDQ Y1, Y8, Y12
    VPSLLQ $32, Y11, Y11
    VPSLLQ $32, Y12, Y12
    VPOR Y11, Y7, Y7
    VPOR Y12, Y8, Y8
    VPSUBQ Y9, Y5, Y5
    VPSUBQ Y10, Y6, Y6

generic_loop:
    VMOVDQU (BX), Y1
    VPERMD Y1, Y7, Y2
    VPERMD Y1, Y8, Y3
    VPSRLVQ Y5, Y2, Y2
    VPSRLVQ Y6, Y3, Y3
    VPAND Y0, Y2, Y2
    VPAND Y0, Y3, Y3
    VMOVDQU Y2, (AX)(SI*8)
    VMOVDQU Y3, 32(AX)(SI*8)
    ADDQ CX, BX
    ADDQ $8, SI
    CMPQ SI, DI
    JNE generic_loop
    VZEROUPPER

    CMPQ SI, DX
    JE done
    LEAQ (AX)(SI*8), AX
    SUBQ SI, DX
tail:
    MOVQ AX, dst_base-56(SP)
    MOVQ DX, dst_len-48(SP)
    MOVQ BX, src_base-32(SP)
    MOVQ CX, bitWidth-8(SP)
    CALL ·unpackInt64Default(SB)
done:
    RET

GLOBL range0n7<>(SB), RODATA|NOPTR, $64
DATA range0n7<>+0(SB)/8,  $0
DATA range0n7<>+8(SB)/8,  $1
DATA range0n7<>+16(SB)/8, $2
DATA range0n7<>+24(SB)/8, $3
DATA range0n7<>+32(SB)/8, $4
DATA range0n7<>+40(SB)/8, $5
DATA range0n7<>+48(SB)/8, $6
DATA range0n7<>+56(SB)/8, $7
