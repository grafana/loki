//go:build !purego

#include "textflag.h"

// func Count(data []byte, value byte) int
TEXT ·Count(SB), NOSPLIT, $0-40
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    MOVB value+24(FP), BX
    MOVQ CX, DX // len
    ADDQ AX, CX // end
    XORQ SI, SI // count

    CMPQ DX, $256
    JB test

    CMPB ·hasAVX2(SB), $0
    JE test

    XORQ R12, R12
    XORQ R13, R13
    XORQ R14, R14
    XORQ DI, DI

    CMPB ·hasAVX512Count(SB), $0
    JE initAVX2

    SHRQ $8, DX
    SHLQ $8, DX
    ADDQ AX, DX
    VPBROADCASTB BX, Z0
loopAVX512:
    VMOVDQU64 (AX), Z1
    VMOVDQU64 64(AX), Z2
    VMOVDQU64 128(AX), Z3
    VMOVDQU64 192(AX), Z4
    VPCMPUB $0, Z0, Z1, K1
    VPCMPUB $0, Z0, Z2, K2
    VPCMPUB $0, Z0, Z3, K3
    VPCMPUB $0, Z0, Z4, K4
    KMOVQ K1, R8
    KMOVQ K2, R9
    KMOVQ K3, R10
    KMOVQ K4, R11
    POPCNTQ R8, R8
    POPCNTQ R9, R9
    POPCNTQ R10, R10
    POPCNTQ R11, R11
    ADDQ R8, R12
    ADDQ R9, R13
    ADDQ R10, R14
    ADDQ R11, DI
    ADDQ $256, AX
    CMPQ AX, DX
    JNE loopAVX512
    ADDQ R12, R13
    ADDQ R14, DI
    ADDQ R13, SI
    ADDQ DI, SI
    JMP doneAVX

initAVX2:
    SHRQ $6, DX
    SHLQ $6, DX
    ADDQ AX, DX
    VPBROADCASTB value+24(FP), Y0
loopAVX2:
    VMOVDQU (AX), Y1
    VMOVDQU 32(AX), Y2
    VPCMPEQB Y0, Y1, Y1
    VPCMPEQB Y0, Y2, Y2
    VPMOVMSKB Y1, R12
    VPMOVMSKB Y2, R13
    POPCNTL R12, R12
    POPCNTL R13, R13
    ADDQ R12, R14
    ADDQ R13, DI
    ADDQ $64, AX
    CMPQ AX, DX
    JNE loopAVX2
    ADDQ R14, SI
    ADDQ DI, SI

doneAVX:
    VZEROUPPER
    JMP test

loop:
    MOVQ SI, DI
    INCQ DI
    MOVB (AX), R8
    CMPB BX, R8
    CMOVQEQ DI, SI
    INCQ AX
test:
    CMPQ AX, CX
    JNE loop
done:
    MOVQ SI, ret+32(FP)
    RET
