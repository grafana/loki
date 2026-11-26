//go:build !purego && !goexperiment.simd

#include "textflag.h"

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
