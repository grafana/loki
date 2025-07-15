//go:build !purego

#include "textflag.h"

#define errnoIndexOutOfBounds 1

// func dictionaryBoundsInt32(dict []int32, indexes []int32) (min, max int32, err errno)
TEXT ·dictionaryBoundsInt32(SB), NOSPLIT, $0-64
    MOVQ dict_base+0(FP), AX
    MOVQ dict_len+8(FP), BX

    MOVQ indexes_base+24(FP), CX
    MOVQ indexes_len+32(FP), DX

    XORQ R10, R10 // min
    XORQ R11, R11 // max
    XORQ R12, R12 // err
    XORQ SI, SI

    CMPQ DX, $0
    JE return

    MOVL (CX), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVL (AX)(DI*4), R10
    MOVL R10, R11

    CMPQ DX, $8
    JB test

    CMPB ·hasAVX512VL(SB), $0
    JE test

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI

    MOVQ $0xFFFF, R8
    KMOVW R8, K1

    VPBROADCASTD BX, Y2  // [len(dict)...]
    VPBROADCASTD R10, Y3 // [min...]
    VMOVDQU32 Y3, Y4     // [max...]
loopAVX512:
    VMOVDQU32 (CX)(SI*4), Y0
    VPCMPUD $1, Y2, Y0, K2
    KMOVW K2, R9
    CMPB R9, $0xFF
    JNE indexOutOfBounds
    VPGATHERDD (AX)(Y0*4), K1, Y1
    VPMINSD Y1, Y3, Y3
    VPMAXSD Y1, Y4, Y4
    KMOVW R8, K1
    ADDQ $8, SI
    CMPQ SI, DI
    JNE loopAVX512

    VPERM2I128 $1, Y3, Y3, Y0
    VPERM2I128 $1, Y4, Y4, Y1
    VPMINSD Y0, Y3, Y3
    VPMAXSD Y1, Y4, Y4

    VPSHUFD $0b1110, Y3, Y0
    VPSHUFD $0b1110, Y4, Y1
    VPMINSD Y0, Y3, Y3
    VPMAXSD Y1, Y4, Y4

    VPSHUFD $1, Y3, Y0
    VPSHUFD $1, Y4, Y1
    VPMINSD Y0, Y3, Y3
    VPMAXSD Y1, Y4, Y4

    MOVQ X3, R10
    MOVQ X4, R11
    ANDQ $0xFFFFFFFF, R10
    ANDQ $0xFFFFFFFF, R11

    VZEROUPPER
    JMP test
loop:
    MOVL (CX)(SI*4), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVL (AX)(DI*4), DI
    CMPL DI, R10
    CMOVLLT DI, R10
    CMPL DI, R11
    CMOVLGT DI, R11
    INCQ SI
test:
    CMPQ SI, DX
    JNE loop
return:
    MOVL R10, min+48(FP)
    MOVL R11, max+52(FP)
    MOVQ R12, err+56(FP)
    RET
indexOutOfBounds:
    MOVQ $errnoIndexOutOfBounds, R12
    JMP return

// func dictionaryBoundsInt64(dict []int64, indexes []int32) (min, max int64, err errno)
TEXT ·dictionaryBoundsInt64(SB), NOSPLIT, $0-72
    MOVQ dict_base+0(FP), AX
    MOVQ dict_len+8(FP), BX

    MOVQ indexes_base+24(FP), CX
    MOVQ indexes_len+32(FP), DX

    XORQ R10, R10 // min
    XORQ R11, R11 // max
    XORQ R12, R12 // err
    XORQ SI, SI

    CMPQ DX, $0
    JE return

    MOVL (CX), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVQ (AX)(DI*8), R10
    MOVQ R10, R11

    CMPQ DX, $8
    JB test

    CMPB ·hasAVX512VL(SB), $0
    JE test

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI

    MOVQ $0xFFFF, R8
    KMOVW R8, K1

    VPBROADCASTD BX, Y2  // [len(dict)...]
    VPBROADCASTQ R10, Z3 // [min...]
    VMOVDQU64 Z3, Z4     // [max...]
loopAVX512:
    VMOVDQU32 (CX)(SI*4), Y0
    VPCMPUD $1, Y2, Y0, K2
    KMOVW K2, R9
    CMPB R9, $0xFF
    JNE indexOutOfBounds
    VPGATHERDQ (AX)(Y0*8), K1, Z1
    VPMINSQ Z1, Z3, Z3
    VPMAXSQ Z1, Z4, Z4
    KMOVW R8, K1
    ADDQ $8, SI
    CMPQ SI, DI
    JNE loopAVX512

    VPERMQ $0b1110, Z3, Z0
    VPERMQ $0b1110, Z4, Z1
    VPMINSQ Z0, Z3, Z3
    VPMAXSQ Z1, Z4, Z4

    VPERMQ $1, Z3, Z0
    VPERMQ $1, Z4, Z1
    VPMINSQ Z0, Z3, Z3
    VPMAXSQ Z1, Z4, Z4

    VSHUFF64X2 $2, Z3, Z3, Z0
    VSHUFF64X2 $2, Z4, Z4, Z1
    VPMINSQ Z0, Z3, Z3
    VPMAXSQ Z1, Z4, Z4

    MOVQ X3, R10
    MOVQ X4, R11

    VZEROUPPER
    JMP test
loop:
    MOVL (CX)(SI*4), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVQ (AX)(DI*8), DI
    CMPQ DI, R10
    CMOVQLT DI, R10
    CMPQ DI, R11
    CMOVQGT DI, R11
    INCQ SI
test:
    CMPQ SI, DX
    JNE loop
return:
    MOVQ R10, min+48(FP)
    MOVQ R11, max+56(FP)
    MOVQ R12, err+64(FP)
    RET
indexOutOfBounds:
    MOVQ $errnoIndexOutOfBounds, R12
    JMP return

// func dictionaryBoundsFloat32(dict []float32, indexes []int32) (min, max float32, err errno)
TEXT ·dictionaryBoundsFloat32(SB), NOSPLIT, $0-64
    MOVQ dict_base+0(FP), AX
    MOVQ dict_len+8(FP), BX

    MOVQ indexes_base+24(FP), CX
    MOVQ indexes_len+32(FP), DX

    PXOR X3, X3   // min
    PXOR X4, X4   // max
    XORQ R12, R12 // err
    XORQ SI, SI

    CMPQ DX, $0
    JE return

    MOVL (CX), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVSS (AX)(DI*4), X3
    MOVAPS X3, X4

    CMPQ DX, $8
    JB test

    CMPB ·hasAVX512VL(SB), $0
    JE test

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI

    MOVQ $0xFFFF, R8
    KMOVW R8, K1

    VPBROADCASTD BX, Y2 // [len(dict)...]
    VPBROADCASTD X3, Y3 // [min...]
    VMOVDQU32 Y3, Y4    // [max...]
loopAVX512:
    VMOVDQU32 (CX)(SI*4), Y0
    VPCMPUD $1, Y2, Y0, K2
    KMOVW K2, R9
    CMPB R9, $0xFF
    JNE indexOutOfBounds
    VPGATHERDD (AX)(Y0*4), K1, Y1
    VMINPS Y1, Y3, Y3
    VMAXPS Y1, Y4, Y4
    KMOVW R8, K1
    ADDQ $8, SI
    CMPQ SI, DI
    JNE loopAVX512

    VPERM2I128 $1, Y3, Y3, Y0
    VPERM2I128 $1, Y4, Y4, Y1
    VMINPS Y0, Y3, Y3
    VMAXPS Y1, Y4, Y4

    VPSHUFD $0b1110, Y3, Y0
    VPSHUFD $0b1110, Y4, Y1
    VMINPS Y0, Y3, Y3
    VMAXPS Y1, Y4, Y4

    VPSHUFD $1, Y3, Y0
    VPSHUFD $1, Y4, Y1
    VMINPS Y0, Y3, Y3
    VMAXPS Y1, Y4, Y4

    VZEROUPPER
    JMP test
loop:
    MOVL (CX)(SI*4), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVSS (AX)(DI*4), X1
    UCOMISS X3, X1
    JAE skipAssignMin
    MOVAPS X1, X3
skipAssignMin:
    UCOMISS X4, X1
    JBE skipAssignMax
    MOVAPS X1, X4
skipAssignMax:
    INCQ SI
test:
    CMPQ SI, DX
    JNE loop
return:
    MOVSS X3, min+48(FP)
    MOVSS X4, max+52(FP)
    MOVQ R12, err+56(FP)
    RET
indexOutOfBounds:
    MOVQ $errnoIndexOutOfBounds, R12
    JMP return

// func dictionaryBoundsFloat64(dict []float64, indexes []int32) (min, max float64, err errno)
TEXT ·dictionaryBoundsFloat64(SB), NOSPLIT, $0-72
    MOVQ dict_base+0(FP), AX
    MOVQ dict_len+8(FP), BX

    MOVQ indexes_base+24(FP), CX
    MOVQ indexes_len+32(FP), DX

    PXOR X3, X3   // min
    PXOR X4, X4   // max
    XORQ R12, R12 // err
    XORQ SI, SI

    CMPQ DX, $0
    JE return

    MOVL (CX), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVSD (AX)(DI*8), X3
    MOVAPS X3, X4

    CMPQ DX, $8
    JB test

    CMPB ·hasAVX512VL(SB), $0
    JE test

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI

    MOVQ $0xFFFF, R8
    KMOVW R8, K1

    VPBROADCASTD BX, Y2 // [len(dict)...]
    VPBROADCASTQ X3, Z3 // [min...]
    VMOVDQU64 Z3, Z4    // [max...]
loopAVX512:
    VMOVDQU32 (CX)(SI*4), Y0
    VPCMPUD $1, Y2, Y0, K2
    KMOVW K2, R9
    CMPB R9, $0xFF
    JNE indexOutOfBounds
    VPGATHERDQ (AX)(Y0*8), K1, Z1
    VMINPD Z1, Z3, Z3
    VMAXPD Z1, Z4, Z4
    KMOVW R8, K1
    ADDQ $8, SI
    CMPQ SI, DI
    JNE loopAVX512

    VPERMQ $0b1110, Z3, Z0
    VPERMQ $0b1110, Z4, Z1
    VMINPD Z0, Z3, Z3
    VMAXPD Z1, Z4, Z4

    VPERMQ $1, Z3, Z0
    VPERMQ $1, Z4, Z1
    VMINPD Z0, Z3, Z3
    VMAXPD Z1, Z4, Z4

    VSHUFF64X2 $2, Z3, Z3, Z0
    VSHUFF64X2 $2, Z4, Z4, Z1
    VMINPD Z0, Z3, Z3
    VMAXPD Z1, Z4, Z4

    VZEROUPPER
    JMP test
loop:
    MOVL (CX)(SI*4), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVSD (AX)(DI*8), X1
    UCOMISD X3, X1
    JAE skipAssignMin
    MOVAPD X1, X3
skipAssignMin:
    UCOMISD X4, X1
    JBE skipAssignMax
    MOVAPD X1, X4
skipAssignMax:
    INCQ SI
test:
    CMPQ SI, DX
    JNE loop
return:
    MOVSD X3, min+48(FP)
    MOVSD X4, max+56(FP)
    MOVQ R12, err+64(FP)
    RET
indexOutOfBounds:
    MOVQ $errnoIndexOutOfBounds, R12
    JMP return

// func dictionaryBoundsUint32(dict []uint32, indexes []int32) (min, max uint32, err errno)
TEXT ·dictionaryBoundsUint32(SB), NOSPLIT, $0-64
    MOVQ dict_base+0(FP), AX
    MOVQ dict_len+8(FP), BX

    MOVQ indexes_base+24(FP), CX
    MOVQ indexes_len+32(FP), DX

    XORQ R10, R10 // min
    XORQ R11, R11 // max
    XORQ R12, R12 // err
    XORQ SI, SI

    CMPQ DX, $0
    JE return

    MOVL (CX), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVL (AX)(DI*4), R10
    MOVL R10, R11

    CMPQ DX, $8
    JB test

    CMPB ·hasAVX512VL(SB), $0
    JE test

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI

    MOVQ $0xFFFF, R8
    KMOVW R8, K1

    VPBROADCASTD BX, Y2  // [len(dict)...]
    VPBROADCASTD R10, Y3 // [min...]
    VMOVDQU32 Y3, Y4     // [max...]
loopAVX512:
    VMOVDQU32 (CX)(SI*4), Y0
    VPCMPUD $1, Y2, Y0, K2
    KMOVW K2, R9
    CMPB R9, $0xFF
    JNE indexOutOfBounds
    VPGATHERDD (AX)(Y0*4), K1, Y1
    VPMINUD Y1, Y3, Y3
    VPMAXUD Y1, Y4, Y4
    KMOVW R8, K1
    ADDQ $8, SI
    CMPQ SI, DI
    JNE loopAVX512

    VPERM2I128 $1, Y3, Y3, Y0
    VPERM2I128 $1, Y4, Y4, Y1
    VPMINUD Y0, Y3, Y3
    VPMAXUD Y1, Y4, Y4

    VPSHUFD $0b1110, Y3, Y0
    VPSHUFD $0b1110, Y4, Y1
    VPMINUD Y0, Y3, Y3
    VPMAXUD Y1, Y4, Y4

    VPSHUFD $1, Y3, Y0
    VPSHUFD $1, Y4, Y1
    VPMINUD Y0, Y3, Y3
    VPMAXUD Y1, Y4, Y4

    MOVQ X3, R10
    MOVQ X4, R11
    ANDQ $0xFFFFFFFF, R10
    ANDQ $0xFFFFFFFF, R11

    VZEROUPPER
    JMP test
loop:
    MOVL (CX)(SI*4), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVL (AX)(DI*4), DI
    CMPL DI, R10
    CMOVLCS DI, R10
    CMPL DI, R11
    CMOVLHI DI, R11
    INCQ SI
test:
    CMPQ SI, DX
    JNE loop
return:
    MOVL R10, min+48(FP)
    MOVL R11, max+52(FP)
    MOVQ R12, err+56(FP)
    RET
indexOutOfBounds:
    MOVQ $errnoIndexOutOfBounds, R12
    JMP return

// func dictionaryBoundsUint64(dict []uint64, indexes []int32) (min, max uint64, err errno)
TEXT ·dictionaryBoundsUint64(SB), NOSPLIT, $0-72
    MOVQ dict_base+0(FP), AX
    MOVQ dict_len+8(FP), BX

    MOVQ indexes_base+24(FP), CX
    MOVQ indexes_len+32(FP), DX

    XORQ R10, R10 // min
    XORQ R11, R11 // max
    XORQ R12, R12 // err
    XORQ SI, SI

    CMPQ DX, $0
    JE return

    MOVL (CX)(SI*4), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVQ (AX)(DI*8), R10
    MOVQ R10, R11

    CMPQ DX, $8
    JB test

    CMPB ·hasAVX512VL(SB), $0
    JE test

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI

    MOVQ $0xFFFF, R8
    KMOVW R8, K1

    VPBROADCASTD BX, Y2  // [len(dict)...]
    VPBROADCASTQ R10, Z3 // [min...]
    VMOVDQU64 Z3, Z4     // [max...]
loopAVX512:
    VMOVDQU32 (CX)(SI*4), Y0
    VPCMPUD $1, Y2, Y0, K2
    KMOVW K2, R9
    CMPB R9, $0xFF
    JNE indexOutOfBounds
    VPGATHERDQ (AX)(Y0*8), K1, Z1
    VPMINUQ Z1, Z3, Z3
    VPMAXUQ Z1, Z4, Z4
    KMOVW R8, K1
    ADDQ $8, SI
    CMPQ SI, DI
    JNE loopAVX512

    VPERMQ $0b1110, Z3, Z0
    VPERMQ $0b1110, Z4, Z1
    VPMINUQ Z0, Z3, Z3
    VPMAXUQ Z1, Z4, Z4

    VPERMQ $1, Z3, Z0
    VPERMQ $1, Z4, Z1
    VPMINUQ Z0, Z3, Z3
    VPMAXUQ Z1, Z4, Z4

    VSHUFF64X2 $2, Z3, Z3, Z0
    VSHUFF64X2 $2, Z4, Z4, Z1
    VPMINUQ Z0, Z3, Z3
    VPMAXUQ Z1, Z4, Z4

    MOVQ X3, R10
    MOVQ X4, R11

    VZEROUPPER
    JMP test
loop:
    MOVL (CX)(SI*4), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVQ (AX)(DI*8), DI
    CMPQ DI, R10
    CMOVQCS DI, R10
    CMPQ DI, R11
    CMOVQHI DI, R11
    INCQ SI
test:
    CMPQ SI, DX
    JNE loop
return:
    MOVQ R10, min+48(FP)
    MOVQ R11, max+56(FP)
    MOVQ R12, err+64(FP)
    RET
indexOutOfBounds:
    MOVQ $errnoIndexOutOfBounds, R12
    JMP return

// func dictionaryBoundsBE128(dict [][16]byte, indexes []int32) (min, max *[16]byte, err errno)
TEXT ·dictionaryBoundsBE128(SB), NOSPLIT, $0-72
    MOVQ dict_base+0(FP), AX
    MOVQ dict_len+8(FP), BX

    MOVQ indexes_base+24(FP), CX
    MOVQ indexes_len+32(FP), DX
    SHLQ $2, DX // x 4
    ADDQ CX, DX // end

    XORQ R8, R8 // min (pointer)
    XORQ R9, R9 // max (pointer)
    XORQ SI, SI // err
    XORQ DI, DI

    CMPQ DX, $0
    JE return

    MOVL (CX), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    SHLQ $4, DI // the dictionary contains 16 byte words
    LEAQ (AX)(DI*1), R8
    MOVQ R8, R9
    MOVQ 0(AX)(DI*1), R10 // min (high)
    MOVQ 8(AX)(DI*1), R11 // min (low)
    BSWAPQ R10
    BSWAPQ R11
    MOVQ R10, R12 // max (high)
    MOVQ R11, R13 // max (low)

    JMP next
loop:
    MOVL (CX), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    SHLQ $4, DI
    MOVQ 0(AX)(DI*1), R14
    MOVQ 8(AX)(DI*1), R15
    BSWAPQ R14
    BSWAPQ R15
testLessThan:
    CMPQ R14, R10
    JA testGreaterThan
    JB lessThan
    CMPQ R15, R11
    JAE testGreaterThan
lessThan:
    LEAQ (AX)(DI*1), R8
    MOVQ R14, R10
    MOVQ R15, R11
    JMP next
testGreaterThan:
    CMPQ R14, R12
    JB next
    JA greaterThan
    CMPQ R15, R13
    JBE next
greaterThan:
    LEAQ (AX)(DI*1), R9
    MOVQ R14, R12
    MOVQ R15, R13
next:
    ADDQ $4, CX
    CMPQ CX, DX
    JNE loop
return:
    MOVQ R8, min+48(FP)
    MOVQ R9, max+56(FP)
    MOVQ SI, err+64(FP)
    RET
indexOutOfBounds:
    MOVQ $errnoIndexOutOfBounds, SI
    JMP return

// The lookup functions provide optimized versions of the dictionary index
// lookup logic.
//
// When AVX512 is available, the AVX512 versions of the functions are used
// which use the VPGATHER* instructions to perform 8 parallel lookups of the
// values in the dictionary, then VPSCATTER* to do 8 parallel writes to the
// sparse output buffer.

// func dictionaryLookup32(dict []uint32, indexes []int32, rows sparse.Array) errno
TEXT ·dictionaryLookup32(SB), NOSPLIT, $0-80
    MOVQ dict_base+0(FP), AX
    MOVQ dict_len+8(FP), BX

    MOVQ indexes_base+24(FP), CX
    MOVQ indexes_len+32(FP), DX

    MOVQ rows_array_ptr+48(FP), R8
    MOVQ rows_array_off+64(FP), R9

    XORQ SI, SI

    CMPQ DX, $8
    JB test

    CMPB ·hasAVX512VL(SB), $0
    JE test

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI

    MOVQ R9, R10
    SHLQ $3, R10 // 8 * size

    MOVW $0xFFFF, R11
    KMOVW R11, K1
    KMOVW R11, K2

    VPBROADCASTD R9, Y2           // [size...]
    VPMULLD ·range0n8(SB), Y2, Y2 // [0*size,1*size,...]
    VPBROADCASTD BX, Y3           // [len(dict)...]
loopAVX512:
    VMOVDQU32 (CX)(SI*4), Y0
    VPCMPUD $1, Y3, Y0, K3
    KMOVW K3, R11
    CMPB R11, $0xFF
    JNE indexOutOfBounds
    VPGATHERDD (AX)(Y0*4), K1, Y1
    VPSCATTERDD Y1, K2, (R8)(Y2*1)
    KMOVW R11, K1
    KMOVW R11, K2
    ADDQ R10, R8
    ADDQ $8, SI
    CMPQ SI, DI
    JNE loopAVX512
    VZEROUPPER
    JMP test
loop:
    MOVL (CX)(SI*4), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVL (AX)(DI*4), DI
    MOVL DI, (R8)
    ADDQ R9, R8
    INCQ SI
test:
    CMPQ SI, DX
    JNE loop
    XORQ AX, AX
return:
    MOVQ AX, ret+72(FP)
    RET
indexOutOfBounds:
    MOVQ $errnoIndexOutOfBounds, AX
    JMP return

// func dictionaryLookup64(dict []uint64, indexes []int32, rows sparse.Array) errno
TEXT ·dictionaryLookup64(SB), NOSPLIT, $0-80
    MOVQ dict_base+0(FP), AX
    MOVQ dict_len+8(FP), BX

    MOVQ indexes_base+24(FP), CX
    MOVQ indexes_len+32(FP), DX

    MOVQ rows_array_ptr+48(FP), R8
    MOVQ rows_array_off+64(FP), R9

    XORQ SI, SI

    CMPQ DX, $8
    JB test

    CMPB ·hasAVX512VL(SB), $0
    JE test

    MOVQ DX, DI
    SHRQ $3, DI
    SHLQ $3, DI

    MOVQ R9, R10
    SHLQ $3, R10 // 8 * size

    MOVW $0xFFFF, R11
    KMOVW R11, K1
    KMOVW R11, K2

    VPBROADCASTD R9, Y2           // [size...]
    VPMULLD ·range0n8(SB), Y2, Y2 // [0*size,1*size,...]
    VPBROADCASTD BX, Y3           // [len(dict)...]
loopAVX512:
    VMOVDQU32 (CX)(SI*4), Y0
    VPCMPUD $1, Y3, Y0, K3
    KMOVW K3, R11
    CMPB R11, $0xFF
    JNE indexOutOfBounds
    VPGATHERDQ (AX)(Y0*8), K1, Z1
    VPSCATTERDQ Z1, K2, (R8)(Y2*1)
    KMOVW R11, K1
    KMOVW R11, K2
    ADDQ R10, R8
    ADDQ $8, SI
    CMPQ SI, DI
    JNE loopAVX512
    VZEROUPPER
    JMP test
loop:
    MOVL (CX)(SI*4), DI
    CMPL DI, BX
    JAE indexOutOfBounds
    MOVQ (AX)(DI*8), DI
    MOVQ DI, (R8)
    ADDQ R9, R8
    INCQ SI
test:
    CMPQ SI, DX
    JNE loop
    XORQ AX, AX
return:
    MOVQ AX, ret+72(FP)
    RET
indexOutOfBounds:
    MOVQ $errnoIndexOutOfBounds, AX
    JMP return

// func dictionaryLookupByteArrayString(dict []uint32, page []byte, indexes []int32, rows sparse.Array) errno
TEXT ·dictionaryLookupByteArrayString(SB), NOSPLIT, $0-104
    MOVQ dict_base+0(FP), AX
    MOVQ dict_len+8(FP), BX
    DECQ BX // the offsets have the total length as last element

    MOVQ page_base+24(FP), CX

    MOVQ indexes_base+48(FP), R8
    MOVQ indexes_len+56(FP), R9

    MOVQ rows_array_ptr+72(FP), R10
    MOVQ rows_array_off+88(FP), R11

    XORQ DI, DI
    XORQ SI, SI
loop:
    // Load the index that we want to read the value from. This may come from
    // user input so we must validate that the indexes are within the bounds of
    // the dictionary.
    MOVL (R8)(SI*4), DI
    CMPL DI, BX
    JAE indexOutOfBounds

    // Load the offsets within the dictionary page where the value is stored.
    // We trust the offsets to be correct since they are generated internally by
    // the dictionary code, there is no need to check that they are within the
    // bounds of the dictionary page.
    MOVL 0(AX)(DI*4), DX
    MOVL 4(AX)(DI*4), DI

    // Compute the length of the value (the difference between two consecutive
    // offsets), and the pointer to the first byte of the string value.
    SUBL DX, DI
    LEAQ (CX)(DX*1), DX

    // Store the length and pointer to the value into the output location.
    // The memory layout is expected to hold a pointer and length, which are
    // both 64 bits words. This is the layout used by parquet.Value and the Go
    // string value type.
    MOVQ DX, (R10)
    MOVQ DI, 8(R10)

    ADDQ R11, R10
    INCQ SI
test:
    CMPQ SI, R9
    JNE loop
    XORQ AX, AX
return:
    MOVQ AX, ret+96(FP)
    RET
indexOutOfBounds:
    MOVQ $errnoIndexOutOfBounds, AX
    JMP return

// func dictionaryLookupFixedLenByteArrayString(dict []byte, len int, indexes []int32, rows sparse.Array) errno
TEXT ·dictionaryLookupFixedLenByteArrayString(SB), NOSPLIT, $0-88
    MOVQ dict_base+0(FP), AX
    MOVQ dict_len+8(FP), BX

    MOVQ len+24(FP), CX

    MOVQ indexes_base+32(FP), DX
    MOVQ indexes_len+40(FP), R8

    MOVQ rows_array_ptr+56(FP), R9
    MOVQ rows_array_off+72(FP), R10

    XORQ DI, DI
    XORQ SI, SI
loop:
    MOVL (DX)(SI*4), DI
    IMULQ CX, DI
    CMPL DI, BX
    JAE indexOutOfBounds

    ADDQ AX, DI
    MOVQ DI, (R9)
    MOVQ CX, 8(R9)

    ADDQ R10, R9
    INCQ SI
test:
    CMPQ SI, R8
    JNE loop
    XORQ AX, AX
return:
    MOVQ AX, ret+80(FP)
    RET
indexOutOfBounds:
    MOVQ $errnoIndexOutOfBounds, AX
    JMP return

// This is the same algorithm as dictionaryLookupFixedLenByteArrayString but we
// only store the pointer to the location holding the value instead of storing
// the pair of pointer and length. Since the length is fixed for this dictionary
// type, the application can assume it at the call site.
//
// func dictionaryLookupFixedLenByteArrayPointer(dict []byte, len int, indexes []int32, rows sparse.Array) errno
TEXT ·dictionaryLookupFixedLenByteArrayPointer(SB), NOSPLIT, $0-88
    MOVQ dict_base+0(FP), AX
    MOVQ dict_len+8(FP), BX

    MOVQ len+24(FP), CX

    MOVQ indexes_base+32(FP), DX
    MOVQ indexes_len+40(FP), R8

    MOVQ rows_array_ptr+56(FP), R9
    MOVQ rows_array_off+72(FP), R10

    XORQ DI, DI
    XORQ SI, SI
loop:
    MOVL (DX)(SI*4), DI
    IMULQ CX, DI
    CMPL DI, BX
    JAE indexOutOfBounds

    ADDQ AX, DI
    MOVQ DI, (R9)

    ADDQ R10, R9
    INCQ SI
test:
    CMPQ SI, R8
    JNE loop
    XORQ AX, AX
return:
    MOVQ AX, ret+80(FP)
    RET
indexOutOfBounds:
    MOVQ $errnoIndexOutOfBounds, AX
    JMP return

GLOBL ·range0n8(SB), RODATA|NOPTR, $40
DATA ·range0n8+0(SB)/4, $0
DATA ·range0n8+4(SB)/4, $1
DATA ·range0n8+8(SB)/4, $2
DATA ·range0n8+12(SB)/4, $3
DATA ·range0n8+16(SB)/4, $4
DATA ·range0n8+20(SB)/4, $5
DATA ·range0n8+24(SB)/4, $6
DATA ·range0n8+28(SB)/4, $7
DATA ·range0n8+32(SB)/4, $8
