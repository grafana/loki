//go:build !purego

#include "textflag.h"

#define bswap128lo 0x08080A0B0C0D0E0F
#define bswap128hi 0x0001020304050607

DATA bswap128+0(SB)/8, $bswap128lo
DATA bswap128+8(SB)/8, $bswap128hi
DATA bswap128+16(SB)/8, $bswap128lo
DATA bswap128+24(SB)/8, $bswap128hi
DATA bswap128+32(SB)/8, $bswap128lo
DATA bswap128+40(SB)/8, $bswap128hi
DATA bswap128+48(SB)/8, $bswap128lo
DATA bswap128+56(SB)/8, $bswap128hi
GLOBL bswap128(SB), RODATA|NOPTR, $64

DATA indexes128+0(SB)/8, $0
DATA indexes128+8(SB)/8, $0
DATA indexes128+16(SB)/8, $1
DATA indexes128+24(SB)/8, $1
DATA indexes128+32(SB)/8, $2
DATA indexes128+40(SB)/8, $2
DATA indexes128+48(SB)/8, $3
DATA indexes128+56(SB)/8, $3
GLOBL indexes128(SB), RODATA|NOPTR, $64

DATA swap64+0(SB)/8, $4
DATA swap64+8(SB)/8, $5
DATA swap64+16(SB)/8, $6
DATA swap64+24(SB)/8, $7
DATA swap64+32(SB)/8, $2
DATA swap64+40(SB)/8, $3
DATA swap64+48(SB)/8, $0
DATA swap64+56(SB)/8, $1
GLOBL swap64(SB), RODATA|NOPTR, $64

DATA swap32+0(SB)/4, $8
DATA swap32+4(SB)/4, $9
DATA swap32+8(SB)/4, $10
DATA swap32+12(SB)/4, $11
DATA swap32+16(SB)/4, $12
DATA swap32+20(SB)/4, $13
DATA swap32+24(SB)/4, $14
DATA swap32+28(SB)/4, $15
DATA swap32+32(SB)/4, $4
DATA swap32+36(SB)/4, $5
DATA swap32+40(SB)/4, $6
DATA swap32+44(SB)/4, $7
DATA swap32+48(SB)/4, $2
DATA swap32+52(SB)/4, $3
DATA swap32+56(SB)/4, $0
DATA swap32+60(SB)/4, $1
GLOBL swap32(SB), RODATA|NOPTR, $64

// func combinedBoundsInt32(data []int32) (min, max int32)
TEXT ·combinedBoundsInt32(SB), NOSPLIT, $-32
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    XORQ R8, R8
    XORQ R9, R9

    CMPQ CX, $0
    JE done
    XORQ SI, SI
    MOVLQZX (AX), R8 // min
    MOVLQZX (AX), R9 // max

    CMPB ·hasAVX512VL(SB), $0
    JE loop

    CMPQ CX, $32
    JB loop

    MOVQ CX, DI
    SHRQ $5, DI
    SHLQ $5, DI
    VPBROADCASTD (AX), Z0
    VPBROADCASTD (AX), Z3
loop32:
    VMOVDQU32 (AX)(SI*4), Z1
    VMOVDQU32 64(AX)(SI*4), Z2
    VPMINSD Z1, Z0, Z0
    VPMINSD Z2, Z0, Z0
    VPMAXSD Z1, Z3, Z3
    VPMAXSD Z2, Z3, Z3
    ADDQ $32, SI
    CMPQ SI, DI
    JNE loop32

    VMOVDQU32 swap32+0(SB), Z1
    VMOVDQU32 swap32+0(SB), Z2
    VPERMI2D Z0, Z0, Z1
    VPERMI2D Z3, Z3, Z2
    VPMINSD Y1, Y0, Y0
    VPMAXSD Y2, Y3, Y3

    VMOVDQU32 swap32+32(SB), Y1
    VMOVDQU32 swap32+32(SB), Y2
    VPERMI2D Y0, Y0, Y1
    VPERMI2D Y3, Y3, Y2
    VPMINSD X1, X0, X0
    VPMAXSD X2, X3, X3

    VMOVDQU32 swap32+48(SB), X1
    VMOVDQU32 swap32+48(SB), X2
    VPERMI2D X0, X0, X1
    VPERMI2D X3, X3, X2
    VPMINSD X1, X0, X0
    VPMAXSD X2, X3, X3
    VZEROUPPER

    MOVQ X0, BX
    MOVQ X3, DX
    MOVL BX, R8
    MOVL DX, R9
    SHRQ $32, BX
    SHRQ $32, DX
    CMPL BX, R8
    CMOVLLT BX, R8
    CMPL DX, R9
    CMOVLGT DX, R9

    CMPQ SI, CX
    JE done
loop:
    MOVLQZX (AX)(SI*4), DX
    CMPL DX, R8
    CMOVLLT DX, R8
    CMPL DX, R9
    CMOVLGT DX, R9
    INCQ SI
    CMPQ SI, CX
    JNE loop
done:
    MOVL R8, min+24(FP)
    MOVL R9, max+28(FP)
    RET

// func combinedBoundsInt64(data []int64) (min, max int64)
TEXT ·combinedBoundsInt64(SB), NOSPLIT, $-40
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    XORQ R8, R8
    XORQ R9, R9

    CMPQ CX, $0
    JE done
    XORQ SI, SI
    MOVQ (AX), R8 // min
    MOVQ (AX), R9 // max

    CMPB ·hasAVX512VL(SB), $0
    JE loop

    CMPQ CX, $16
    JB loop

    MOVQ CX, DI
    SHRQ $4, DI
    SHLQ $4, DI
    VPBROADCASTQ (AX), Z0
    VPBROADCASTQ (AX), Z3
loop16:
    VMOVDQU64 (AX)(SI*8), Z1
    VMOVDQU64 64(AX)(SI*8), Z2
    VPMINSQ Z1, Z0, Z0
    VPMINSQ Z2, Z0, Z0
    VPMAXSQ Z1, Z3, Z3
    VPMAXSQ Z2, Z3, Z3
    ADDQ $16, SI
    CMPQ SI, DI
    JNE loop16

    VMOVDQU32 swap32+0(SB), Z1
    VMOVDQU32 swap32+0(SB), Z2
    VPERMI2D Z0, Z0, Z1
    VPERMI2D Z3, Z3, Z2
    VPMINSQ Y1, Y0, Y0
    VPMAXSQ Y2, Y3, Y3

    VMOVDQU32 swap32+32(SB), Y1
    VMOVDQU32 swap32+32(SB), Y2
    VPERMI2D Y0, Y0, Y1
    VPERMI2D Y3, Y3, Y2
    VPMINSQ X1, X0, X0
    VPMAXSQ X2, X3, X3

    VMOVDQU32 swap32+48(SB), X1
    VMOVDQU32 swap32+48(SB), X2
    VPERMI2D X0, X0, X1
    VPERMI2D X3, X3, X2
    VPMINSQ X1, X0, X0
    VPMAXSQ X2, X3, X3
    VZEROUPPER

    MOVQ X0, R8
    MOVQ X3, R9
    CMPQ SI, CX
    JE done
loop:
    MOVQ (AX)(SI*8), DX
    CMPQ DX, R8
    CMOVQLT DX, R8
    CMPQ DX, R9
    CMOVQGT DX, R9
    INCQ SI
    CMPQ SI, CX
    JNE loop
done:
    MOVQ R8, min+24(FP)
    MOVQ R9, max+32(FP)
    RET

// func combinedBoundsUint32(data []uint32) (min, max uint32)
TEXT ·combinedBoundsUint32(SB), NOSPLIT, $-32
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    XORQ R8, R8
    XORQ R9, R9

    CMPQ CX, $0
    JE done
    XORQ SI, SI
    MOVLQZX (AX), R8 // min
    MOVLQZX (AX), R9 // max

    CMPB ·hasAVX512VL(SB), $0
    JE loop

    CMPQ CX, $32
    JB loop

    MOVQ CX, DI
    SHRQ $5, DI
    SHLQ $5, DI
    VPBROADCASTD (AX), Z0
    VPBROADCASTD (AX), Z3
loop32:
    VMOVDQU32 (AX)(SI*4), Z1
    VMOVDQU32 64(AX)(SI*4), Z2
    VPMINUD Z1, Z0, Z0
    VPMINUD Z2, Z0, Z0
    VPMAXUD Z1, Z3, Z3
    VPMAXUD Z2, Z3, Z3
    ADDQ $32, SI
    CMPQ SI, DI
    JNE loop32

    VMOVDQU32 swap32+0(SB), Z1
    VMOVDQU32 swap32+0(SB), Z2
    VPERMI2D Z0, Z0, Z1
    VPERMI2D Z3, Z3, Z2
    VPMINUD Y1, Y0, Y0
    VPMAXUD Y2, Y3, Y3

    VMOVDQU32 swap32+32(SB), Y1
    VMOVDQU32 swap32+32(SB), Y2
    VPERMI2D Y0, Y0, Y1
    VPERMI2D Y3, Y3, Y2
    VPMINUD X1, X0, X0
    VPMAXUD X2, X3, X3

    VMOVDQU32 swap32+48(SB), X1
    VMOVDQU32 swap32+48(SB), X2
    VPERMI2D X0, X0, X1
    VPERMI2D X3, X3, X2
    VPMINUD X1, X0, X0
    VPMAXUD X2, X3, X3
    VZEROUPPER

    MOVQ X0, BX
    MOVQ X3, DX
    MOVL BX, R8
    MOVL DX, R9
    SHRQ $32, BX
    SHRQ $32, DX
    CMPL BX, R8
    CMOVLCS BX, R8
    CMPL DX, R9
    CMOVLHI DX, R9

    CMPQ SI, CX
    JE done
loop:
    MOVLQZX (AX)(SI*4), DX
    CMPL DX, R8
    CMOVLCS DX, R8
    CMPL DX, R9
    CMOVLHI DX, R9
    INCQ SI
    CMPQ SI, CX
    JNE loop
done:
    MOVL R8, min+24(FP)
    MOVL R9, max+28(FP)
    RET

// func combinedBoundsUint64(data []uint64) (min, max uint64)
TEXT ·combinedBoundsUint64(SB), NOSPLIT, $-40
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    XORQ R8, R8
    XORQ R9, R9

    CMPQ CX, $0
    JE done
    XORQ SI, SI
    MOVQ (AX), R8 // min
    MOVQ (AX), R9 // max

    CMPB ·hasAVX512VL(SB), $0
    JE loop

    CMPQ CX, $16
    JB loop

    MOVQ CX, DI
    SHRQ $4, DI
    SHLQ $4, DI
    VPBROADCASTQ (AX), Z0
    VPBROADCASTQ (AX), Z3
loop16:
    VMOVDQU64 (AX)(SI*8), Z1
    VMOVDQU64 64(AX)(SI*8), Z2
    VPMINUQ Z1, Z0, Z0
    VPMINUQ Z2, Z0, Z0
    VPMAXUQ Z1, Z3, Z3
    VPMAXUQ Z2, Z3, Z3
    ADDQ $16, SI
    CMPQ SI, DI
    JNE loop16

    VMOVDQU32 swap32+0(SB), Z1
    VMOVDQU32 swap32+0(SB), Z2
    VPERMI2D Z0, Z0, Z1
    VPERMI2D Z3, Z3, Z2
    VPMINUQ Y1, Y0, Y0
    VPMAXUQ Y2, Y3, Y3

    VMOVDQU32 swap32+32(SB), Y1
    VMOVDQU32 swap32+32(SB), Y2
    VPERMI2D Y0, Y0, Y1
    VPERMI2D Y3, Y3, Y2
    VPMINUQ X1, X0, X0
    VPMAXUQ X2, X3, X3

    VMOVDQU32 swap32+48(SB), X1
    VMOVDQU32 swap32+48(SB), X2
    VPERMI2D X0, X0, X1
    VPERMI2D X3, X3, X2
    VPMINUQ X1, X0, X0
    VPMAXUQ X2, X3, X3
    VZEROUPPER

    MOVQ X0, R8
    MOVQ X3, R9
    CMPQ SI, CX
    JE done
loop:
    MOVQ (AX)(SI*8), DX
    CMPQ DX, R8
    CMOVQCS DX, R8
    CMPQ DX, R9
    CMOVQHI DX, R9
    INCQ SI
    CMPQ SI, CX
    JNE loop
done:
    MOVQ R8, min+24(FP)
    MOVQ R9, max+32(FP)
    RET

// func combinedBoundsFloat32(data []float32) (min, max float32)
TEXT ·combinedBoundsFloat32(SB), NOSPLIT, $-32
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    XORQ R8, R8
    XORQ R9, R9

    CMPQ CX, $0
    JE done
    XORPS X0, X0
    XORPS X1, X1
    XORQ SI, SI
    MOVLQZX (AX), R8 // min
    MOVLQZX (AX), R9 // max
    MOVQ R8, X0
    MOVQ R9, X1

    CMPB ·hasAVX512VL(SB), $0
    JE loop

    CMPQ CX, $32
    JB loop

    MOVQ CX, DI
    SHRQ $5, DI
    SHLQ $5, DI
    VPBROADCASTD (AX), Z0
    VPBROADCASTD (AX), Z3
loop32:
    VMOVDQU32 (AX)(SI*4), Z1
    VMOVDQU32 64(AX)(SI*4), Z2
    VMINPS Z1, Z0, Z0
    VMINPS Z2, Z0, Z0
    VMAXPS Z1, Z3, Z3
    VMAXPS Z2, Z3, Z3
    ADDQ $32, SI
    CMPQ SI, DI
    JNE loop32

    VMOVDQU32 swap32+0(SB), Z1
    VMOVDQU32 swap32+0(SB), Z2
    VPERMI2D Z0, Z0, Z1
    VPERMI2D Z3, Z3, Z2
    VMINPS Y1, Y0, Y0
    VMAXPS Y2, Y3, Y3

    VMOVDQU32 swap32+32(SB), Y1
    VMOVDQU32 swap32+32(SB), Y2
    VPERMI2D Y0, Y0, Y1
    VPERMI2D Y3, Y3, Y2
    VMINPS X1, X0, X0
    VMAXPS X2, X3, X3

    VMOVDQU32 swap32+48(SB), X1
    VMOVDQU32 swap32+48(SB), X2
    VPERMI2D X0, X0, X1
    VPERMI2D X3, X3, X2
    VMINPS X1, X0, X0
    VMAXPS X2, X3, X3
    VZEROUPPER

    MOVAPS X0, X1
    MOVAPS X3, X2

    PSRLQ $32, X1
    MOVQ X0, R8
    MOVQ X1, R10
    UCOMISS X0, X1
    CMOVLCS R10, R8

    PSRLQ $32, X2
    MOVQ X3, R9
    MOVQ X2, R11
    UCOMISS X3, X2
    CMOVLHI R11, R9

    CMPQ SI, CX
    JE done
    MOVQ R8, X0
    MOVQ R9, X1
loop:
    MOVLQZX (AX)(SI*4), DX
    MOVQ DX, X2
    UCOMISS X0, X2
    CMOVLCS DX, R8
    UCOMISS X1, X2
    CMOVLHI DX, R9
    MOVQ R8, X0
    MOVQ R9, X1
    INCQ SI
    CMPQ SI, CX
    JNE loop
done:
    MOVL R8, min+24(FP)
    MOVL R9, max+28(FP)
    RET

// func combinedBoundsFloat64(data []float64) (min, max float64)
TEXT ·combinedBoundsFloat64(SB), NOSPLIT, $-40
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    XORQ R8, R8
    XORQ R9, R9

    CMPQ CX, $0
    JE done
    XORPD X0, X0
    XORPD X1, X1
    XORQ SI, SI
    MOVQ (AX), R8 // min
    MOVQ (AX), R9 // max
    MOVQ R8, X0
    MOVQ R9, X1

    CMPB ·hasAVX512VL(SB), $0
    JE loop

    CMPQ CX, $16
    JB loop

    MOVQ CX, DI
    SHRQ $4, DI
    SHLQ $4, DI
    VPBROADCASTQ (AX), Z0
    VPBROADCASTQ (AX), Z3
loop16:
    VMOVDQU64 (AX)(SI*8), Z1
    VMOVDQU64 64(AX)(SI*8), Z2
    VMINPD Z1, Z0, Z0
    VMINPD Z2, Z0, Z0
    VMAXPD Z1, Z3, Z3
    VMAXPD Z2, Z3, Z3
    ADDQ $16, SI
    CMPQ SI, DI
    JNE loop16

    VMOVDQU64 swap32+0(SB), Z1
    VMOVDQU64 swap32+0(SB), Z2
    VPERMI2D Z0, Z0, Z1
    VPERMI2D Z3, Z3, Z2
    VMINPD Y1, Y0, Y0
    VMAXPD Y2, Y3, Y3

    VMOVDQU64 swap32+32(SB), Y1
    VMOVDQU64 swap32+32(SB), Y2
    VPERMI2D Y0, Y0, Y1
    VPERMI2D Y3, Y3, Y2
    VMINPD X1, X0, X0
    VMAXPD X2, X3, X3

    VMOVDQU64 swap32+48(SB), X1
    VMOVDQU64 swap32+48(SB), X2
    VPERMI2D X0, X0, X1
    VPERMI2D X3, X3, X2
    VMINPD X1, X0, X0
    VMAXPD X2, X3, X1
    VZEROUPPER

    MOVQ X0, R8
    MOVQ X1, R9
    CMPQ SI, CX
    JE done
loop:
    MOVQ (AX)(SI*8), DX
    MOVQ DX, X2
    UCOMISD X0, X2
    CMOVQCS DX, R8
    UCOMISD X1, X2
    CMOVQHI DX, R9
    MOVQ R8, X0
    MOVQ R9, X1
    INCQ SI
    CMPQ SI, CX
    JNE loop
done:
    MOVQ R8, min+24(FP)
    MOVQ R9, max+32(FP)
    RET
