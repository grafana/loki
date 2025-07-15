//go:build !purego

#include "textflag.h"

// func encodeByteArrayLengths(lengths []int32, offsets []uint32)
TEXT ·encodeByteArrayLengths(SB), NOSPLIT, $0-48
    MOVQ lengths_base+0(FP), AX
    MOVQ lengths_len+8(FP), CX
    MOVQ offsets_base+24(FP), BX
    XORQ SI, SI

    CMPQ CX, $4
    JB test

    MOVQ CX, DX
    SHRQ $2, DX
    SHLQ $2, DX

loopSSE2:
    MOVOU 0(BX)(SI*4), X0
    MOVOU 4(BX)(SI*4), X1
    PSUBL X0, X1
    MOVOU X1, (AX)(SI*4)
    ADDQ $4, SI
    CMPQ SI, DX
    JNE loopSSE2
    JMP test
loop:
    MOVL 0(BX)(SI*4), R8
    MOVL 4(BX)(SI*4), R9
    SUBL R8, R9
    MOVL R9, (AX)(SI*4)
    INCQ SI
test:
    CMPQ SI, CX
    JNE loop
    RET

// func decodeByteArrayLengths(offsets []uint32, length []int32) (lastOffset uint32, invalidLength int32)
TEXT ·decodeByteArrayLengths(SB), NOSPLIT, $0-56
    MOVQ offsets_base+0(FP), AX
    MOVQ lengths_base+24(FP), BX
    MOVQ lengths_len+32(FP), CX

    XORQ DX, DX // lastOffset
    XORQ DI, DI // invalidLength
    XORQ SI, SI

    CMPQ CX, $4
    JL test

    MOVQ CX, R8
    SHRQ $2, R8
    SHLQ $2, R8

    MOVL $0, (AX)
    PXOR X0, X0
    PXOR X3, X3
    // This loop computes the prefix sum of the lengths array in order to
    // generate values of the offsets array.
    //
    // We stick to SSE2 to keep the code simple (the Go compiler appears to
    // assume that SSE2 must be supported on AMD64) which already yields most
    // of the performance that we could get on this subroutine if we were using
    // AVX2.
    //
    // The X3 register also accumulates a mask of all length values, which is
    // checked after the loop to determine whether any of the lengths were
    // negative.
    //
    // The following article contains a description of the prefix sum algorithm
    // used in this function: https://en.algorithmica.org/hpc/algorithms/prefix/
loopSSE2:
    MOVOU (BX)(SI*4), X1
    POR X1, X3

    MOVOA X1, X2
    PSLLDQ $4, X2
    PADDD X2, X1

    MOVOA X1, X2
    PSLLDQ $8, X2
    PADDD X2, X1

    PADDD X1, X0
    MOVOU X0, 4(AX)(SI*4)

    PSHUFD $0b11111111, X0, X0

    ADDQ $4, SI
    CMPQ SI, R8
    JNE loopSSE2

    // If any of the most significant bits of double words in the X3 register
    // are set to 1, it indicates that one of the lengths was negative and
    // therefore the prefix sum is invalid.
    //
    // TODO: we report the invalid length as -1, effectively losing the original
    // value due to the aggregation within X3. This is something that we might
    // want to address in the future to provide better error reporting.
    MOVMSKPS X3, R8
    MOVL $-1, R9
    CMPL R8, $0
    CMOVLNE R9, DI

    MOVQ X0, DX
    JMP test
loop:
    MOVL (BX)(SI*4), R8
    MOVL DX, (AX)(SI*4)
    ADDL R8, DX
    CMPL R8, $0
    CMOVLLT R8, DI
    INCQ SI
test:
    CMPQ SI, CX
    JNE loop

    MOVL DX, (AX)(SI*4)
    MOVL DX, lastOffset+48(FP)
    MOVL DI, invalidLength+52(FP)
    RET
