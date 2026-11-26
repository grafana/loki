//go:build !purego && !goexperiment.simd

#include "textflag.h"

// func gatherBitsAVX2(dst []byte, src Uint8Array)
TEXT ·gatherBitsAVX2(SB), NOSPLIT, $0-48
    MOVQ dst_base+0(FP), AX
    MOVQ src_array_ptr+24(FP), BX
    MOVQ src_array_len+32(FP), CX
    MOVQ src_array_off+40(FP), DX
    XORQ SI, SI
    SHRQ $3, CX

    VPBROADCASTD src_array_off+40(FP), Y0
    VPMULLD range0n7<>(SB), Y0, Y0
    VPCMPEQD Y1, Y1, Y1
    VPCMPEQD Y2, Y2, Y2
loop:
    VPGATHERDD Y1, (BX)(Y0*1), Y3
    VMOVDQU Y2, Y1
    VPSLLD $31, Y3, Y3
    VMOVMSKPS Y3, DI

    MOVB DI, (AX)(SI*1)

    LEAQ (BX)(DX*8), BX
    INCQ SI
    CMPQ SI, CX
    JNE loop
    VZEROUPPER
    RET

// func gatherBitsDefault(dst []byte, src Uint8Array)
TEXT ·gather32AVX2(SB), NOSPLIT, $0-48
    MOVQ dst_base+0(FP), AX
    MOVQ dst_len+8(FP), CX
    MOVQ src_array_ptr+24(FP), BX
    MOVQ src_array_off+40(FP), DX
    XORQ SI, SI

    VPBROADCASTD src_array_off+40(FP), Y0
    VPMULLD range0n7<>(SB), Y0, Y0
    VPCMPEQD Y1, Y1, Y1
    VPCMPEQD Y2, Y2, Y2
loop:
    VPGATHERDD Y1, (BX)(Y0*1), Y3
    VMOVDQU Y3, (AX)(SI*4)
    VMOVDQU Y2, Y1

    LEAQ (BX)(DX*8), BX
    ADDQ $8, SI
    CMPQ SI, CX
    JNE loop
    VZEROUPPER
    RET

// func gather64AVX2(dst []uint64, src Uint64Array)
TEXT ·gather64AVX2(SB), NOSPLIT, $0-48
    MOVQ dst_base+0(FP), AX
    MOVQ dst_len+8(FP), CX
    MOVQ src_array_ptr+24(FP), BX
    MOVQ src_array_off+40(FP), DX
    XORQ SI, SI

    VPBROADCASTQ src_array_off+40(FP), Y0
    VPMULLD range0n3<>(SB), Y0, Y0
    VPCMPEQQ Y1, Y1, Y1
    VPCMPEQQ Y2, Y2, Y2
loop:
    VPGATHERQQ Y1, (BX)(Y0*1), Y3
    VMOVDQU Y3, (AX)(SI*8)
    VMOVDQU Y2, Y1

    LEAQ (BX)(DX*4), BX
    ADDQ $4, SI
    CMPQ SI, CX
    JNE loop
    VZEROUPPER
    RET

// func gather128(dst [][16]byte, src Uint128Array) int

GLOBL range0n3<>(SB), RODATA|NOPTR, $32
DATA range0n3<>+0(SB)/8,  $0
DATA range0n3<>+8(SB)/8,  $1
DATA range0n3<>+16(SB)/8, $2
DATA range0n3<>+24(SB)/8, $3
GLOBL range0n7<>(SB), RODATA|NOPTR, $32
DATA range0n7<>+0(SB)/4,  $0
DATA range0n7<>+4(SB)/4,  $1
DATA range0n7<>+8(SB)/4,  $2
DATA range0n7<>+12(SB)/4, $3
DATA range0n7<>+16(SB)/4, $4
DATA range0n7<>+20(SB)/4, $5
DATA range0n7<>+24(SB)/4, $6
DATA range0n7<>+28(SB)/4, $7
