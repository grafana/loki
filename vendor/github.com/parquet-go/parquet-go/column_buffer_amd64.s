//go:build !purego

#include "textflag.h"

// func broadcastRangeInt32AVX2(dst []int32, base int32)
TEXT ·broadcastRangeInt32AVX2(SB), NOSPLIT, $0-28
    MOVQ dst_base+0(FP), AX
    MOVQ dst_len+8(FP), BX
    MOVL base+24(FP), CX
    XORQ SI, SI

    CMPQ BX, $8
    JB test1x4

    VMOVDQU ·range0n8(SB), Y0         // [0,1,2,3,4,5,6,7]
    VPBROADCASTD ·range0n8+32(SB), Y1 // [8,8,8,8,8,8,8,8]
    VPBROADCASTD base+24(FP), Y2      // [base...]
    VPADDD Y2, Y0, Y0                 // [base,base+1,...]

    MOVQ BX, DI
    SHRQ $3, DI
    SHLQ $3, DI
    JMP test8x4
loop8x4:
    VMOVDQU Y0, (AX)(SI*4)
    VPADDD Y1, Y0, Y0
    ADDQ $8, SI
test8x4:
    CMPQ SI, DI
    JNE loop8x4
    VZEROUPPER
    JMP test1x4

loop1x4:
    INCQ SI
    MOVL CX, DX
    IMULL SI, DX
    MOVL DX, -4(AX)(SI*4)
test1x4:
    CMPQ SI, BX
    JNE loop1x4
    RET

// func writePointersBE128(values [][16]byte, rows sparse.Array)
TEXT ·writePointersBE128(SB), NOSPLIT, $0-48
    MOVQ values_base+0(FP), AX
    MOVQ rows_array_ptr+24(FP), BX
    MOVQ rows_array_len+32(FP), CX
    MOVQ rows_array_off+40(FP), DX

    XORQ SI, SI
    JMP test
loop:
    PXOR X0, X0
    MOVQ (BX), DI // *[16]byte
    CMPQ DI, $0
    JE next
    MOVOU (DI), X0
next:
    MOVOU X0, (AX)
    ADDQ $16, AX
    ADDQ DX, BX
    INCQ SI
test:
    CMPQ SI, CX
    JNE loop
    RET
