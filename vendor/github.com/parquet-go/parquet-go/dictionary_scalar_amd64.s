//go:build !purego && !goexperiment.simd

#include "textflag.h"

#define errnoIndexOutOfBounds 1

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
