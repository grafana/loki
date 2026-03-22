//go:build !purego

#include "textflag.h"

#define sizeOfValue 24

// This function is an optimized implementation of the memsetValues function
// which assigns the parquet.Value passed as second argument to all elements of
// the first slice argument.
//
// The optimizations relies on the fact that we can pack 4 parquet.Value values
// into 3 YMM registers (24 x 4 = 32 x 3 = 96).
//
// func memsetValuesAVX2(values []Value, model Value, _ uint64)
TEXT ·memsetValuesAVX2(SB), NOSPLIT, $0-56 // 48 + padding to load model in YMM
    MOVQ values_base+0(FP), AX
    MOVQ values_len+8(FP), BX

    MOVQ model_ptr+24(FP), R10
    MOVQ model_u64+32(FP), R11
    MOVQ model+40(FP), R12 // go vet complains about this line but it's OK

    XORQ SI, SI // byte index
    MOVQ BX, DI // byte count
    IMULQ $sizeOfValue, DI

    CMPQ BX, $4
    JB test

    MOVQ BX, R8
    SHRQ $2, R8
    SHLQ $2, R8
    IMULQ $sizeOfValue, R8

    VMOVDQU model+24(FP), Y0
    VMOVDQU Y0, Y1
    VMOVDQU Y0, Y2

    VPERMQ $0b00100100, Y0, Y0
    VPERMQ $0b01001001, Y1, Y1
    VPERMQ $0b10010010, Y2, Y2
loop4:
    VMOVDQU Y0, 0(AX)(SI*1)
    VMOVDQU Y1, 32(AX)(SI*1)
    VMOVDQU Y2, 64(AX)(SI*1)
    ADDQ $4*sizeOfValue, SI
    CMPQ SI, R8
    JNE loop4
    VZEROUPPER
    JMP test
loop:
    MOVQ R10, 0(AX)(SI*1)
    MOVQ R11, 8(AX)(SI*1)
    MOVQ R12, 16(AX)(SI*1)
    ADDQ $sizeOfValue, SI
test:
    CMPQ SI, DI
    JNE loop
    RET
