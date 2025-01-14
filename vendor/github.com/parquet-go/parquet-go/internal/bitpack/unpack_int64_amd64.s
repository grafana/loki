//go:build !purego

#include "funcdata.h"
#include "textflag.h"

// func unpackInt64Default(dst []int64, src []uint32, bitWidth uint)
TEXT ·unpackInt64Default(SB), NOSPLIT, $0-56
    MOVQ dst_base+0(FP), AX
    MOVQ dst_len+8(FP), DX
    MOVQ src_base+24(FP), BX
    MOVQ bitWidth+48(FP), CX

    MOVQ $1, R8 // bitMask = (1 << bitWidth) - 1
    SHLQ CX, R8, R8
    DECQ R8
    MOVQ CX, R9 // bitWidth

    XORQ DI, DI // bitOffset
    XORQ SI, SI // index
    XORQ R10, R10
    XORQ R11, R11
    XORQ R14, R14
    JMP test
loop:
    MOVQ DI, R10
    MOVQ DI, CX
    SHRQ $5, R10      // i = bitOffset / 32
    ANDQ $0b11111, CX // j = bitOffset % 32

    MOVLQZX (BX)(R10*4), R11
    MOVQ R8, R12  // d = bitMask
    SHLQ CX, R12  // d = d << j
    ANDQ R12, R11 // d = src[i] & d
    SHRQ CX, R11  // d = d >> j

    MOVQ CX, R13
    ADDQ R9, R13
    CMPQ R13, $32
    JBE next // j+bitWidth <= 32 ?
    MOVQ CX, R15 // j

    MOVLQZX 4(BX)(R10*4), R14
    MOVQ $32, CX
    SUBQ R15, CX  // k = 32 - j
    MOVQ R8, R12  // c = bitMask
    SHRQ CX, R12  // c = c >> k
    ANDQ R12, R14 // c = src[i+1] & c
    SHLQ CX, R14  // c = c << k
    ORQ R14, R11  // d = d | c

    CMPQ R13, $64
    JBE next

    MOVLQZX 8(BX)(R10*4), R14
    MOVQ $64, CX
    SUBQ R15, CX  // k = 64 - j
    MOVQ R8, R12  // c = bitMask
    SHRQ CX, R12  // c = c >> k
    ANDQ R12, R14 // c = src[i+2] & c
    SHLQ CX, R14  // c = c << k
    ORQ R14, R11  // d = d | c
next:
    MOVQ R11, (AX)(SI*8) // dst[n] = d
    ADDQ R9, DI          // bitOffset += bitWidth
    INCQ SI
test:
    CMPQ SI, DX
    JNE loop
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
// func unpackInt64x1to32bitsAVX2(dst []int64, src []byte, bitWidth uint)
TEXT ·unpackInt64x1to32bitsAVX2(SB), NOSPLIT, $56-56
    NO_LOCAL_POINTERS
    MOVQ dst_base+0(FP), AX
    MOVQ dst_len+8(FP), DX
    MOVQ src_base+24(FP), BX
    MOVQ bitWidth+48(FP), CX

    CMPQ DX, $8
    JB tail

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI
    XORQ SI, SI

    MOVQ $1, R8
    SHLQ CX, R8
    DECQ R8
    MOVQ R8, X0
    VPBROADCASTQ X0, Y0 // bitMask = (1 << bitWidth) - 1

    VPCMPEQQ Y1, Y1, Y1
    VPSRLQ $63, Y1, Y1  // [1,1,1,1]

    MOVQ CX, X2
    VPBROADCASTQ X2, Y2 // [bitWidth]

    VMOVDQU range0n7<>+0(SB), Y3  // [0,1,2,3]
    VMOVDQU range0n7<>+32(SB), Y4 // [4,5,6,7]

    VPMULLD Y2, Y3, Y5 // [bitWidth] * [0,1,2,3]
    VPMULLD Y2, Y4, Y6 // [bitWidth] * [4,5,6,7]

    VPSRLQ $5, Y5, Y7 // ([bitWidth] * [0,1,2,3]) / 32
    VPSRLQ $5, Y6, Y8 // ([bitWidth] * [4,5,6,7]) / 32

    VPSLLQ $5, Y7, Y9  // (([bitWidth] * [0,1,2,3]) / 32) * 32
    VPSLLQ $5, Y8, Y10 // (([bitWidth] * [4,5,6,7]) / 32) * 32

    VPADDQ Y1, Y7, Y11
    VPADDQ Y1, Y8, Y12
    VPSLLQ $32, Y11, Y11
    VPSLLQ $32, Y12, Y12
    VPOR Y11, Y7, Y7 // permutations[i] = [i | ((i + 1) << 32)]
    VPOR Y12, Y8, Y8 // permutations[i] = [i | ((i + 1) << 32)]

    VPSUBQ Y9, Y5, Y5 // shifts
    VPSUBQ Y10, Y6, Y6
loop:
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
    JNE loop
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
