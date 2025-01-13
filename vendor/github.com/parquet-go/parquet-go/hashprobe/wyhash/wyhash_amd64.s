//go:build !purego

#include "textflag.h"

#define m1 0xa0761d6478bd642f
#define m2 0xe7037ed1a0b428db
#define m3 0x8ebc6af09c88c6e3
#define m4 0x589965cc75374cc3
#define m5 0x1d8e4e27c47d124f

// func MultiHashUint32Array(hashes []uintptr, values sparse.Uint32Array, seed uintptr)
TEXT ·MultiHashUint32Array(SB), NOSPLIT, $0-56
    MOVQ hashes_base+0(FP), R12
    MOVQ values_array_ptr+24(FP), R13
    MOVQ values_array_len+32(FP), R14
    MOVQ values_array_off+40(FP), R15
    MOVQ seed+48(FP), R11

    MOVQ $m1, R8
    MOVQ $m2, R9
    MOVQ $m5^4, R10
    XORQ R11, R8

    XORQ SI, SI
    JMP test
loop:
    MOVL (R13), AX
    MOVQ R8, BX

    XORQ AX, BX
    XORQ R9, AX

    MULQ BX
    XORQ DX, AX

    MULQ R10
    XORQ DX, AX

    MOVQ AX, (R12)(SI*8)
    INCQ SI
    ADDQ R15, R13
test:
    CMPQ SI, R14
    JNE loop
    RET

// func MultiHashUint64Array(hashes []uintptr, values sparse.Uint64Array, seed uintptr)
TEXT ·MultiHashUint64Array(SB), NOSPLIT, $0-56
    MOVQ hashes_base+0(FP), R12
    MOVQ values_array_ptr+24(FP), R13
    MOVQ values_array_len+32(FP), R14
    MOVQ values_array_off+40(FP), R15
    MOVQ seed+48(FP), R11

    MOVQ $m1, R8
    MOVQ $m2, R9
    MOVQ $m5^8, R10
    XORQ R11, R8

    XORQ SI, SI
    JMP test
loop:
    MOVQ (R13), AX
    MOVQ R8, BX

    XORQ AX, BX
    XORQ R9, AX

    MULQ BX
    XORQ DX, AX

    MULQ R10
    XORQ DX, AX

    MOVQ AX, (R12)(SI*8)
    INCQ SI
    ADDQ R15, R13
test:
    CMPQ SI, R14
    JNE loop
    RET

// func MultiHashUint128Array(hashes []uintptr, values sparse.Uint128Array, seed uintptr)
TEXT ·MultiHashUint128Array(SB), NOSPLIT, $0-56
    MOVQ hashes_base+0(FP), R12
    MOVQ values_array_ptr+24(FP), R13
    MOVQ values_array_len+32(FP), R14
    MOVQ values_array_off+40(FP), R15
    MOVQ seed+48(FP), R11

    MOVQ $m1, R8
    MOVQ $m2, R9
    MOVQ $m5^16, R10
    XORQ R11, R8

    XORQ SI, SI
    JMP test
loop:
    MOVQ 0(R13), AX
    MOVQ 8(R13), DX
    MOVQ R8, BX

    XORQ DX, BX
    XORQ R9, AX

    MULQ BX
    XORQ DX, AX

    MULQ R10
    XORQ DX, AX

    MOVQ AX, (R12)(SI*8)
    INCQ SI
    ADDQ R15, R13
test:
    CMPQ SI, R14
    JNE loop
    RET
