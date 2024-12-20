//go:build !purego

#include "textflag.h"

// func broadcastAVX2(dst []byte, src byte)
TEXT ·broadcastAVX2(SB), NOSPLIT, $0-25
    MOVQ dst_base+0(FP), AX
    MOVQ dst_len+8(FP), BX
    MOVBQZX src+24(FP), CX

    CMPQ BX, $8
    JBE test

    CMPQ BX, $64
    JB init8

    XORQ SI, SI
    MOVQ BX, DX
    SHRQ $6, DX
    SHLQ $6, DX
    MOVQ CX, X0
    VPBROADCASTB X0, Y0
loop64:
    VMOVDQU Y0, (AX)(SI*1)
    VMOVDQU Y0, 32(AX)(SI*1)
    ADDQ $64, SI
    CMPQ SI, DX
    JNE loop64
    VMOVDQU Y0, -64(AX)(BX*1)
    VMOVDQU Y0, -32(AX)(BX*1)
    VZEROUPPER
    RET

init8:
    MOVQ $0x0101010101010101, R8
    IMULQ R8, CX
loop8:
    MOVQ CX, -8(AX)(BX*1)
    SUBQ $8, BX
    CMPQ BX, $8
    JAE loop8
    MOVQ CX, (AX)
    RET

loop:
    MOVB CX, -1(AX)(BX*1)
    DECQ BX
test:
    CMPQ BX, $0
    JNE loop
    RET
