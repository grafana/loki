//go:build !purego

#include "textflag.h"

// func nullIndex8(bits *uint64, rows sparse.Array)
TEXT ·nullIndex8(SB), NOSPLIT, $0-32
    MOVQ bits+0(FP), AX
    MOVQ rows_array_ptr+8(FP), BX
    MOVQ rows_array_len+16(FP), DI
    MOVQ rows_array_off+24(FP), DX

    MOVQ $1, CX
    XORQ SI, SI

    CMPQ DI, $0
    JE done
loop1x1:
    XORQ R8, R8
    MOVB (BX), R9
    CMPB R9, $0
    JE next1x1

    MOVQ SI, R10
    SHRQ $6, R10
    ORQ CX, (AX)(R10*8)
next1x1:
    ADDQ DX, BX
    ROLQ $1, CX
    INCQ SI
    CMPQ SI, DI
    JNE loop1x1
done:
    RET

// func nullIndex32(bits *uint64, rows sparse.Array)
TEXT ·nullIndex32(SB), NOSPLIT, $0-32
    MOVQ bits+0(FP), AX
    MOVQ rows_array_ptr+8(FP), BX
    MOVQ rows_array_len+16(FP), DI
    MOVQ rows_array_off+24(FP), DX

    MOVQ $1, CX
    XORQ SI, SI

    CMPQ DI, $0
    JE done

    CMPQ DI, $8
    JB loop1x4

    CMPB ·hasAVX2(SB), $0
    JE loop1x4

    MOVQ DI, R8
    SHRQ $3, R8
    SHLQ $3, R8

    VPBROADCASTD rows_array_off+24(FP), Y0
    VPMULLD ·range0n8(SB), Y0, Y0
    VPCMPEQD Y1, Y1, Y1
    VPCMPEQD Y2, Y2, Y2
    VPXOR Y3, Y3, Y3
loop8x4:
    VPGATHERDD Y1, (BX)(Y0*1), Y4
    VPCMPEQD Y3, Y4, Y4
    VMOVMSKPS Y4, R9
    VMOVDQU Y2, Y1

    NOTQ R9
    ANDQ $0b11111111, R9

    MOVQ SI, CX
    ANDQ $0b111111, CX

    MOVQ SI, R10
    SHRQ $6, R10

    SHLQ CX, R9
    ORQ R9, (AX)(R10*8)

    LEAQ (BX)(DX*8), BX
    ADDQ $8, SI
    CMPQ SI, R8
    JNE loop8x4
    VZEROUPPER

    CMPQ SI, DI
    JE done

    MOVQ $1, R8
    MOVQ SI, CX
    ANDQ $0b111111, R8
    SHLQ CX, R8
    MOVQ R8, CX

loop1x4:
    MOVL (BX), R8
    CMPL R8, $0
    JE next1x4

    MOVQ SI, R9
    SHRQ $6, R9
    ORQ CX, (AX)(R9*8)
next1x4:
    ADDQ DX, BX
    ROLQ $1, CX
    INCQ SI
    CMPQ SI, DI
    JNE loop1x4
done:
    RET

// func nullIndex64(bits *uint64, rows sparse.Array)
TEXT ·nullIndex64(SB), NOSPLIT, $0-32
    MOVQ bits+0(FP), AX
    MOVQ rows_array_ptr+8(FP), BX
    MOVQ rows_array_len+16(FP), DI
    MOVQ rows_array_off+24(FP), DX

    MOVQ $1, CX
    XORQ SI, SI

    CMPQ DI, $0
    JE done

    CMPQ DI, $4
    JB loop1x8

    CMPB ·hasAVX2(SB), $0
    JE loop1x8

    MOVQ DI, R8
    SHRQ $2, R8
    SHLQ $2, R8

    VPBROADCASTQ rows_array_off+24(FP), Y0
    VPMULLD scale4x8<>(SB), Y0, Y0
    VPCMPEQQ Y1, Y1, Y1
    VPCMPEQQ Y2, Y2, Y2
    VPXOR Y3, Y3, Y3
loop4x8:
    VPGATHERQQ Y1, (BX)(Y0*1), Y4
    VPCMPEQQ Y3, Y4, Y4
    VMOVMSKPD Y4, R9
    VMOVDQU Y2, Y1

    NOTQ R9
    ANDQ $0b1111, R9

    MOVQ SI, CX
    ANDQ $0b111111, CX

    MOVQ SI, R10
    SHRQ $6, R10

    SHLQ CX, R9
    ORQ R9, (AX)(R10*8)

    LEAQ (BX)(DX*4), BX
    ADDQ $4, SI
    CMPQ SI, R8
    JNE loop4x8
    VZEROUPPER

    CMPQ SI, DI
    JE done

    MOVQ $1, R8
    MOVQ SI, CX
    ANDQ $0b111111, R8
    SHLQ CX, R8
    MOVQ R8, CX

loop1x8:
    MOVQ (BX), R8
    CMPQ R8, $0
    JE next1x8

    MOVQ SI, R9
    SHRQ $6, R9
    ORQ CX, (AX)(R9*8)
next1x8:
    ADDQ DX, BX
    ROLQ $1, CX
    INCQ SI
    CMPQ SI, DI
    JNE loop1x8
done:
    RET

GLOBL scale4x8<>(SB), RODATA|NOPTR, $32
DATA scale4x8<>+0(SB)/8,  $0
DATA scale4x8<>+8(SB)/8,  $1
DATA scale4x8<>+16(SB)/8, $2
DATA scale4x8<>+24(SB)/8, $3

// func nullIndex128(bits *uint64, rows sparse.Array)
TEXT ·nullIndex128(SB), NOSPLIT, $0-32
    MOVQ bits+0(FP), AX
    MOVQ rows_array_ptr+8(FP), BX
    MOVQ rows_array_len+16(FP), DI
    MOVQ rows_array_off+24(FP), DX

    CMPQ DI, $0
    JE done

    MOVQ $1, CX
    XORQ SI, SI
    PXOR X0, X0
loop1x16:
    MOVOU (BX), X1
    PCMPEQQ X0, X1
    MOVMSKPD X1, R8
    CMPB R8, $0b11
    JE next1x16

    MOVQ SI, R9
    SHRQ $6, R9
    ORQ CX, (AX)(R9*8)
next1x16:
    ADDQ DX, BX
    ROLQ $1, CX
    INCQ SI
    CMPQ SI, DI
    JNE loop1x16
done:
    RET
